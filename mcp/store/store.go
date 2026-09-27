// Package store keeps a local, read-only mirror of the memory repo and
// writes new memories back through the GitHub API.
//
// The mirror is refreshed by downloading a tarball whenever the remote HEAD
// moves, so no git binary is needed. Readers never lock: each sync extracts
// into a fresh snapshot directory and atomically repoints a small "current"
// file at it, which makes concurrent hooks, the MCP server and the web UI safe
// to run side by side.
package store

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/entities"
	gh "github.com/AndrewSkea/team-memory/mcp/github"
	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/policy"
)

// Remote is the subset of the GitHub client the store needs.
type Remote interface {
	HeadSHA(ctx context.Context) (string, error)
	Tarball(ctx context.Context, ref string) (io.ReadCloser, error)
	GetFile(ctx context.Context, path string) (gh.FileInfo, error)
	PutFile(ctx context.Context, path, content, sha, message string) error
	Update(ctx context.Context, path, message string, fn func(string, bool) (string, error)) error
}

const (
	maxFileSize  = 2 << 20
	maxTotalSize = 256 << 20
)

type Store struct {
	dir    string // mirror root for this repo
	remote Remote
}

// New returns a store mirroring into dataDir/mirror/<owner>__<repo>.
func New(dataDir, owner, repo string, remote Remote) *Store {
	return &Store{
		dir:    filepath.Join(dataDir, "mirror", safe(owner)+"__"+safe(repo)),
		remote: remote,
	}
}

func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '.' {
			return '_'
		}
		return r
	}, s)
}

type current struct {
	Dir      string    `json:"dir"`
	SHA      string    `json:"sha"`
	SyncedAt time.Time `json:"synced_at"`
}

func (s *Store) readCurrent() (current, error) {
	var c current
	b, err := os.ReadFile(filepath.Join(s.dir, "current.json"))
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

func (s *Store) writeCurrent(c current) error {
	b, _ := json.Marshal(c)
	return atomicWrite(filepath.Join(s.dir, "current.json"), b)
}

func atomicWrite(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), p); err != nil {
		os.Remove(f.Name())
		return err
	}
	return nil
}

// SyncState describes the mirror.
type SyncState struct {
	SHA      string
	SyncedAt time.Time
	Empty    bool
}

// State returns the last sync state without touching the network.
func (s *Store) State() SyncState {
	c, err := s.readCurrent()
	if err != nil {
		return SyncState{}
	}
	return SyncState{SHA: c.SHA, SyncedAt: c.SyncedAt, Empty: c.SHA == ""}
}

// Sync brings the mirror up to date with the remote HEAD.
func (s *Store) Sync(ctx context.Context) (changed bool, err error) {
	cur, _ := s.readCurrent()
	sha, err := s.remote.HeadSHA(ctx)
	if errors.Is(err, gh.ErrEmptyRepo) {
		empty := current{Dir: "", SHA: "", SyncedAt: time.Now().UTC()}
		return cur.SHA != "", s.writeCurrent(empty)
	}
	if err != nil {
		return false, err
	}
	if sha == cur.SHA && cur.Dir != "" {
		if _, err := os.Stat(filepath.Join(s.dir, cur.Dir)); err == nil {
			cur.SyncedAt = time.Now().UTC()
			return false, s.writeCurrent(cur)
		}
	}
	name := "snap-" + sha[:min(12, len(sha))] + fmt.Sprintf("-%d", time.Now().UnixNano())
	dst := filepath.Join(s.dir, name)
	rc, err := s.remote.Tarball(ctx, sha)
	if err != nil {
		return false, err
	}
	defer rc.Close()
	if err := extract(rc, dst); err != nil {
		os.RemoveAll(dst)
		return false, fmt.Errorf("extract snapshot: %w", err)
	}
	if err := s.writeCurrent(current{Dir: name, SHA: sha, SyncedAt: time.Now().UTC()}); err != nil {
		return false, err
	}
	s.gc(name)
	return true, nil
}

// gc removes old snapshot dirs (keeping a grace period for readers that
// may still be walking one).
func (s *Store) gc(keep string) {
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if !e.IsDir() || e.Name() == keep || !strings.HasPrefix(e.Name(), "snap-") {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 10*time.Minute {
			os.RemoveAll(filepath.Join(s.dir, e.Name()))
		}
	}
}

func extract(r io.Reader, dst string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		// Strip GitHub's "<owner>-<repo>-<sha>/" prefix.
		name := h.Name
		if i := strings.IndexByte(name, '/'); i >= 0 {
			name = name[i+1:]
		}
		name = path.Clean(name)
		if name == "." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") || strings.Contains(name, "/../") {
			continue
		}
		ext := strings.ToLower(path.Ext(name))
		if ext != ".md" && ext != ".yml" && ext != ".yaml" {
			continue
		}
		if h.Size > maxFileSize {
			continue
		}
		total += h.Size
		if total > maxTotalSize {
			return errors.New("repository too large to mirror")
		}
		out := filepath.Join(dst, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, io.LimitReader(tr, maxFileSize)); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
}

// Snapshot is the parsed content of the mirror.
type Snapshot struct {
	SHA       string
	SyncedAt  time.Time
	Memories  []memory.Memory
	Entities  entities.Registry
	Policy    policy.Policy
	Problems  []string // files that failed to parse
	HasLegacy bool
}

// ByID finds a memory.
func (s *Snapshot) ByID(id string) (memory.Memory, bool) {
	for _, m := range s.Memories {
		if m.ID == id {
			return m, true
		}
	}
	return memory.Memory{}, false
}

// skipLegacy are markdown files that are not memory content.
var skipLegacy = map[string]bool{"README.md": true, "INDEX.md": true, "CONTRIBUTING.md": true, "LICENSE.md": true}

// Load parses the current mirror. It never touches the network.
func (s *Store) Load() (*Snapshot, error) {
	cur, err := s.readCurrent()
	snap := &Snapshot{Policy: policy.Default()}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return snap, nil
		}
		return nil, err
	}
	snap.SHA, snap.SyncedAt = cur.SHA, cur.SyncedAt
	if cur.Dir == "" {
		return snap, nil
	}
	root := filepath.Join(s.dir, cur.Dir)
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // snapshot may be gc'd underneath us; best effort
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		switch {
		case rel == entities.Path:
			if r, err := entities.Parse(data); err == nil {
				snap.Entities = r
			} else {
				snap.Problems = append(snap.Problems, rel+": "+err.Error())
			}
		case rel == policy.Path:
			if pol, err := policy.Parse(data); err == nil {
				snap.Policy = pol
			} else {
				snap.Problems = append(snap.Problems, rel+": "+err.Error())
			}
		case !strings.HasSuffix(rel, ".md"):
		case strings.HasPrefix(rel, "memories/"):
			m, err := memory.Parse(rel, string(data))
			if err != nil {
				snap.Problems = append(snap.Problems, err.Error())
				return nil
			}
			snap.Memories = append(snap.Memories, m)
		case strings.HasPrefix(rel, ".") || skipLegacy[path.Base(rel)]:
		default:
			legacy := memory.ParseLegacy(rel, string(data))
			if len(legacy) > 0 {
				snap.HasLegacy = true
				snap.Memories = append(snap.Memories, legacy...)
			}
		}
		return nil
	})
	sort.SliceStable(snap.Memories, func(i, j int) bool {
		return snap.Memories[i].Created.After(snap.Memories[j].Created)
	})
	return snap, err
}

// writeThrough mirrors a successful remote write locally so readers see it
// before the next sync.
func (s *Store) writeThrough(repoPath, content string) {
	cur, err := s.readCurrent()
	if err != nil || cur.Dir == "" {
		return
	}
	_ = atomicWrite(filepath.Join(s.dir, cur.Dir, filepath.FromSlash(repoPath)), []byte(content))
}

// Create commits a new memory file. It fails if the path already exists.
func (s *Store) Create(ctx context.Context, m memory.Memory) (memory.Memory, error) {
	if m.Path == "" {
		m.Path = memory.PathFor(m)
	}
	content, err := memory.Render(m)
	if err != nil {
		return m, err
	}
	msg := fmt.Sprintf("memory: %s", truncate(m.Title, 60))
	if err := s.remote.PutFile(ctx, m.Path, content, "", msg); err != nil {
		return m, err
	}
	s.writeThrough(m.Path, content)
	return m, nil
}

// UpdateMemory rewrites an existing memory via fn, retrying on conflicts.
func (s *Store) UpdateMemory(ctx context.Context, repoPath, message string, fn func(*memory.Memory) error) (memory.Memory, error) {
	var out memory.Memory
	err := s.remote.Update(ctx, repoPath, message, func(cur string, exists bool) (string, error) {
		if !exists {
			return "", fmt.Errorf("memory %s no longer exists", repoPath)
		}
		m, err := memory.Parse(repoPath, cur)
		if err != nil {
			return "", err
		}
		if err := fn(&m); err != nil {
			return "", err
		}
		out = m
		return memory.Render(m)
	})
	if err != nil {
		return out, err
	}
	if content, err := memory.Render(out); err == nil {
		s.writeThrough(repoPath, content)
	}
	return out, nil
}

// UpdateMeta rewrites a meta file (entities.yml, policy.yml).
func (s *Store) UpdateMeta(ctx context.Context, repoPath, message string, fn func(cur string, exists bool) (string, error)) error {
	var final string
	err := s.remote.Update(ctx, repoPath, message, func(cur string, exists bool) (string, error) {
		next, err := fn(cur, exists)
		final = next
		return next, err
	})
	if err == nil {
		s.writeThrough(repoPath, final)
	}
	return err
}

// PutIfMissing creates a file only if it doesn't exist (used for seeding).
func (s *Store) PutIfMissing(ctx context.Context, repoPath, content, message string) (bool, error) {
	f, err := s.remote.GetFile(ctx, repoPath)
	if err != nil {
		return false, err
	}
	if f.Exists {
		return false, nil
	}
	if err := s.remote.PutFile(ctx, repoPath, content, "", message); err != nil {
		if errors.Is(err, gh.ErrConflict) {
			return false, nil
		}
		return false, err
	}
	s.writeThrough(repoPath, content)
	return true, nil
}

func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
