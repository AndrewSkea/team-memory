package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/entities"
	"github.com/AndrewSkea/team-memory/mcp/memory"
)

// ── log ──────────────────────────────────────────────────────────────────────

type rotatingLog struct {
	mu   sync.Mutex
	path string
}

func openLog(path string) io.Writer {
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	return &rotatingLog{path: path}
}

func (l *rotatingLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if info, err := os.Stat(l.path); err == nil && info.Size() > 5<<20 {
		_ = os.Rename(l.path, l.path+".1")
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return len(p), nil // logging must never break the caller
	}
	defer f.Close()
	return f.Write(p)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%s", path, randHex(4))
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var safeRe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

func safeName(s string) string {
	s = safeRe.ReplaceAllString(s, "_")
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

// ── pending review queue ─────────────────────────────────────────────────────

// PendingKind distinguishes new memories from proposed corrections.
const (
	KindMemory     = "memory"
	KindCorrection = "correction"
)

// PendingItem is something waiting for the user's approval. It lives only
// on this machine until approved, so unapproved content never reaches the
// shared repo.
type PendingItem struct {
	Kind        string            `json:"kind"`
	Memory      memory.Memory     `json:"memory"`
	Body        string            `json:"body"`
	Reason      string            `json:"reason"`
	QueuedAt    time.Time         `json:"queued_at"`
	NewEntities []entities.Entity `json:"new_entities,omitempty"`
	// For corrections: the memory being disputed.
	TargetID string `json:"target_id,omitempty"`
	// AutoRetry marks items that were meant to publish but hit a network or
	// GitHub error; they are retried automatically and never need review.
	AutoRetry bool `json:"auto_retry,omitempty"`
}

// ID is the pending item's id (the proposed memory's id).
func (p PendingItem) ID() string { return p.Memory.ID }

func (a *App) pendingDir() string {
	return filepath.Join(a.DataDir, "pending", safeName(a.Config().Owner+"__"+a.Config().Repo))
}

// AddPending queues an item for review.
func (a *App) AddPending(p PendingItem) error {
	p.Body = p.Memory.Body
	return writeJSONFile(filepath.Join(a.pendingDir(), safeName(p.ID())+".json"), p)
}

// ListPending returns items waiting for the user's review, oldest first.
func (a *App) ListPending() ([]PendingItem, error) {
	all, err := a.listPendingAll()
	var out []PendingItem
	for _, p := range all {
		if !p.AutoRetry {
			out = append(out, p)
		}
	}
	return out, err
}

func (a *App) listPendingAll() ([]PendingItem, error) {
	entries, err := os.ReadDir(a.pendingDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []PendingItem
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var p PendingItem
		if err := readJSONFile(filepath.Join(a.pendingDir(), e.Name()), &p); err != nil {
			continue
		}
		p.Memory.Body = p.Body
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].QueuedAt.Before(out[j].QueuedAt) })
	return out, nil
}

// GetPending returns one queued item.
func (a *App) GetPending(id string) (PendingItem, error) {
	var p PendingItem
	err := readJSONFile(filepath.Join(a.pendingDir(), safeName(id)+".json"), &p)
	if errors.Is(err, fs.ErrNotExist) {
		return p, fmt.Errorf("no pending item %q", id)
	}
	p.Memory.Body = p.Body
	return p, err
}

func (a *App) removePending(id string) error {
	err := os.Remove(filepath.Join(a.pendingDir(), safeName(id)+".json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// ── per-session state ────────────────────────────────────────────────────────

// SessionState remembers, per Claude Code session, how much of the
// transcript has been captured and which memories were already shown.
type SessionState struct {
	Offset   int       `json:"offset"`
	Injected []string  `json:"injected,omitempty"`
	Signals  []string  `json:"signals,omitempty"`
	Repo     string    `json:"repo,omitempty"`
	Updated  time.Time `json:"updated"`
}

func (a *App) sessionPath(id string) string {
	return filepath.Join(a.DataDir, "sessions", safeName(id)+".json")
}

func (a *App) LoadSession(id string) SessionState {
	var s SessionState
	if id != "" {
		_ = readJSONFile(a.sessionPath(id), &s)
	}
	return s
}

func (a *App) SaveSession(id string, s SessionState) {
	if id == "" {
		return
	}
	s.Updated = a.Now().UTC()
	if err := writeJSONFile(a.sessionPath(id), s); err != nil {
		a.Log.Printf("save session: %v", err)
	}
}

// CleanSessions deletes session state older than 30 days.
func (a *App) CleanSessions() {
	dir := filepath.Join(a.DataDir, "sessions")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && a.Now().Sub(info.ModTime()) > 30*24*time.Hour {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// ── capture job queue ────────────────────────────────────────────────────────

// Job is a request to capture knowledge from a transcript.
type Job struct {
	SessionID      string    `json:"session_id"`
	TranscriptPath string    `json:"transcript_path"`
	Cwd            string    `json:"cwd"`
	Event          string    `json:"event"`
	QueuedAt       time.Time `json:"queued_at"`
	Attempts       int       `json:"attempts"`
}

func (a *App) queueDir() string { return filepath.Join(a.DataDir, "queue") }

// Enqueue stores a capture job for the background worker.
func (a *App) Enqueue(j Job) error {
	j.QueuedAt = a.Now().UTC()
	name := fmt.Sprintf("%d-%s.json", j.QueuedAt.UnixNano(), randHex(3))
	return writeJSONFile(filepath.Join(a.queueDir(), name), j)
}

// QueueLen returns the number of waiting jobs.
func (a *App) QueueLen() int {
	entries, _ := os.ReadDir(a.queueDir())
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return n
}

// claimJobs atomically claims waiting jobs (and ones abandoned by a crashed
// worker) by renaming them, so concurrent workers never double-process.
func (a *App) claimJobs() []string {
	dir := a.queueDir()
	entries, _ := os.ReadDir(dir)
	var claimed []string
	for _, e := range entries {
		name := e.Name()
		stale := false
		if strings.Contains(name, ".working-") {
			if info, err := e.Info(); err == nil && a.Now().Sub(info.ModTime()) > 15*time.Minute {
				stale = true
			}
		}
		if !strings.HasSuffix(name, ".json") && !stale {
			continue
		}
		base := name
		if i := strings.Index(base, ".working-"); i >= 0 {
			base = base[:i] + ".json"
		}
		dst := filepath.Join(dir, base+".working-"+randHex(4))
		if os.Rename(filepath.Join(dir, name), dst) == nil {
			now := a.Now()
			_ = os.Chtimes(dst, now, now)
			claimed = append(claimed, dst)
		}
	}
	sort.Strings(claimed)
	return claimed
}
