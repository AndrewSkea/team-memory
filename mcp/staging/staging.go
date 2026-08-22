// Package staging holds memories that have not been approved.
//
// Everything here lives under the layout's Staging directory, which is a
// sibling of the git clone. Nothing in this package ever touches the clone.
package staging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/paths"
)

// Area is the staging directory pair: pending and bin.
type Area struct {
	layout paths.Layout
}

// BinItem is a rejected memory plus why and when it was rejected.
type BinItem struct {
	Memory     memory.Memory
	Reason     string
	RejectedAt time.Time
}

// rejection is the sidecar JSON written next to a binned memory. It lives
// beside the memory file, not in a database, so the bin survives being
// inspected and edited by a human with a text editor.
type rejection struct {
	Reason     string    `json:"reason"`
	RejectedAt time.Time `json:"rejected_at"`
}

// New creates the staging directories and returns a handle.
func New(l paths.Layout) (*Area, error) {
	if err := l.EnsureDirs(); err != nil {
		return nil, err
	}
	return &Area{layout: l}, nil
}

// validID rejects anything that is not a plain filename segment. Ids are
// attacker-influenced: they arrive from filenames on disk and, eventually,
// from HTTP request paths, so anything containing a path separator, a ".."
// component, a NUL byte, or a Windows drive letter must fail loudly rather
// than resolve outside Pending/Bin.
func validID(id string) error {
	if id == "" {
		return fmt.Errorf("staging: empty id")
	}
	if strings.ContainsRune(id, 0) {
		return fmt.Errorf("staging: id %q contains a NUL byte", id)
	}
	if strings.ContainsAny(id, `/\:`) {
		return fmt.Errorf("staging: id %q contains a path separator or drive letter", id)
	}
	if id == "." || id == ".." || strings.Contains(id, "..") {
		return fmt.Errorf("staging: id %q contains a parent-directory component", id)
	}
	if id != filepath.Base(id) {
		return fmt.Errorf("staging: id %q is not a plain filename segment", id)
	}
	return nil
}

func (a *Area) pendingPath(id string) string { return filepath.Join(a.layout.Pending, id+".md") }
func (a *Area) binPath(id string) string     { return filepath.Join(a.layout.Bin, id+".md") }
func (a *Area) binMeta(id string) string     { return filepath.Join(a.layout.Bin, id+".json") }

// writeFileAtomic writes data to a temp file in the same directory as path,
// then renames it into place. A reader (Pending/Binned via readDirMemories)
// can therefore never observe a half-written file: it either isn't there yet,
// or it is there complete. Plain os.WriteFile truncates in place first, which
// gives a concurrent reader a window onto a partial file.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup: once the rename below succeeds this is a no-op
	// because tmpName no longer exists.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %q to %q: %w", tmpName, path, err)
	}
	return nil
}

// Add writes a candidate memory into pending, atomically.
func (a *Area) Add(m memory.Memory) error {
	if err := validID(m.ID); err != nil {
		return err
	}
	data, err := memory.Marshal(m)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(a.pendingPath(m.ID), data); err != nil {
		return fmt.Errorf("staging: write pending %s: %w", m.ID, err)
	}
	return nil
}

// Pending lists candidates awaiting review, sorted by SourceSession then
// Created, with ID as a final tiebreak so the listing is a total order and
// therefore deterministic even when two memories share both keys.
func (a *Area) Pending() ([]memory.Memory, error) {
	ms, err := readDirMemories(a.layout.Pending)
	if err != nil {
		return nil, err
	}
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].SourceSession != ms[j].SourceSession {
			return ms[i].SourceSession < ms[j].SourceSession
		}
		if !ms[i].Created.Equal(ms[j].Created) {
			return ms[i].Created.Before(ms[j].Created)
		}
		return ms[i].ID < ms[j].ID
	})
	return ms, nil
}

// Take atomically claims a pending memory, reads it, and removes it. Used
// when a memory is approved and about to be committed.
//
// The claim is an exclusive file create (O_CREATE|O_EXCL) of a fixed
// ".taking" marker in the same directory, not a rename. Both primitives are
// atomic in principle, but under concurrent load in this repo's Windows dev
// environment os.Rename measurably was not: two goroutines racing to Take the
// same id could both observe a nil error from os.Rename (most likely an
// antivirus minifilter duplicating or delaying the underlying IRP — a
// documented class of flakiness for concurrent MoveFileEx on Windows CI
// runners), which would mean a double-commit once the HTTP layer commits
// whatever Take hands back. O_CREATE|O_EXCL is the standard cross-platform
// mutual-exclusion primitive for exactly this kind of claim (it's how lock
// files work everywhere) and did not reproduce the flake under the same
// concurrent-caller test. If two callers race, exactly one create succeeds;
// the loser gets an "already exists" error rather than a second, duplicate
// read of the memory.
func (a *Area) Take(id string) (memory.Memory, error) {
	if err := validID(id); err != nil {
		return memory.Memory{}, err
	}
	p := a.pendingPath(id)
	claim := p + ".taking"

	lock, err := os.OpenFile(claim, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return memory.Memory{}, fmt.Errorf("staging: claim pending %s: %w", id, err)
	}
	lock.Close()
	// The marker's only job is exclusion; once we hold it (or fail to), we
	// no longer need it on disk. Best-effort: a leftover marker after a
	// crash mid-Take is a known, accepted operational gap, not something
	// this call can fix for itself.
	defer os.Remove(claim)

	data, err := os.ReadFile(p)
	if err != nil {
		return memory.Memory{}, fmt.Errorf("staging: read pending %s: %w", id, err)
	}
	m, err := memory.Unmarshal(data)
	if err != nil {
		return memory.Memory{}, fmt.Errorf("staging: parse pending %s: %w", id, err)
	}
	if err := os.Remove(p); err != nil {
		return memory.Memory{}, fmt.Errorf("staging: remove pending %s: %w", id, err)
	}
	return m, nil
}

// Reject moves a pending memory to the bin with a reason. Rejection is
// non-destructive, which is what makes skipping confirmation dialogs safe.
//
// The sidecar is written before the memory file is moved, not after. Binned
// only iterates .md files, so a sidecar that lands before its memory file is
// simply invisible until the memory file exists too — a reader can never
// observe a rejected memory with an empty Reason because the write raced the
// move. The reverse order (move first, sidecar second) has exactly that
// window: a Binned() call between the two steps would report a real, listed
// item with no reason, indistinguishable from a rejection someone genuinely
// gave no reason for.
func (a *Area) Reject(id, reason string) error {
	if err := validID(id); err != nil {
		return err
	}
	meta, err := json.Marshal(rejection{Reason: reason, RejectedAt: time.Now().UTC()})
	if err != nil {
		return fmt.Errorf("staging: encode rejection for %s: %w", id, err)
	}
	if err := writeFileAtomic(a.binMeta(id), meta); err != nil {
		return fmt.Errorf("staging: write rejection for %s: %w", id, err)
	}
	if err := os.Rename(a.pendingPath(id), a.binPath(id)); err != nil {
		// The move failed (e.g. no such pending id): don't leave an orphaned
		// sidecar with no memory behind. Binned() would never surface it
		// anyway (it iterates .md files), but there's no reason to leak it.
		_ = os.Remove(a.binMeta(id))
		return fmt.Errorf("staging: move %s to bin: %w", id, err)
	}
	return nil
}

// Restore moves a binned memory back into pending.
func (a *Area) Restore(id string) error {
	if err := validID(id); err != nil {
		return err
	}
	if err := os.Rename(a.binPath(id), a.pendingPath(id)); err != nil {
		return fmt.Errorf("staging: restore %s: %w", id, err)
	}
	if err := os.Remove(a.binMeta(id)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("staging: clear rejection for %s: %w", id, err)
	}
	return nil
}

// Binned lists rejected memories with their reasons, newest rejection first,
// with Memory.ID as a final tiebreak so the listing is a total order and
// therefore deterministic even when two rejections share the same instant.
func (a *Area) Binned() ([]BinItem, error) {
	ms, err := readDirMemories(a.layout.Bin)
	if err != nil {
		return nil, err
	}
	out := make([]BinItem, 0, len(ms))
	for _, m := range ms {
		item := BinItem{Memory: m}
		// m.ID here is the filename-derived id set by readDirMemories, never
		// the frontmatter id — the sidecar lookup MUST use the same key that
		// Restore and Take use, or a bin file whose frontmatter claims an id
		// like "../../clone/secret" could make Binned read (and surface as
		// review-UI text) a file outside Bin entirely. Frontmatter is file
		// content, and file content is untrusted; the filename the directory
		// listing gave us is not.
		if raw, err := os.ReadFile(a.binMeta(m.ID)); err == nil {
			var r rejection
			if json.Unmarshal(raw, &r) == nil {
				item.Reason = r.Reason
				item.RejectedAt = r.RejectedAt
			}
		}
		out = append(out, item)
	}
	sortBinned(out)
	return out, nil
}

// sortBinned orders bin items newest-rejection-first, with Memory.ID
// ascending as a final tiebreak so the order is total and therefore
// deterministic even when two rejections share the exact same instant.
// Pulled out of Binned so it can be exercised directly against a
// hand-built, already-out-of-order slice: readDirMemories always hands
// Binned entries in filename order, which for this package is identical to
// ID order (a filename is exactly "<id>.md"), so testing the tiebreak
// through Binned's normal disk path can never observe a mis-sort — the
// input is already sorted before the comparator runs.
func sortBinned(items []BinItem) {
	sort.Slice(items, func(i, j int) bool {
		if !items[i].RejectedAt.Equal(items[j].RejectedAt) {
			return items[i].RejectedAt.After(items[j].RejectedAt)
		}
		return items[i].Memory.ID < items[j].Memory.ID
	})
}

// readDirMemories loads every .md file in a directory, skipping unparsable
// ones so a single bad file never hides the rest of the queue.
//
// The returned Memory's ID is always overwritten with the filename-derived
// id (the part before ".md"), never left as whatever the frontmatter inside
// the file happens to claim. Frontmatter is file content — untrusted, and in
// the bin directory specifically, editable by a human with a text editor —
// so any caller that turns a Memory's ID back into a path (Restore, Take, or
// Binned's sidecar lookup) must key off the same value the directory listing
// itself produced, or a crafted/corrupted frontmatter id can walk the path
// outside Pending/Bin. A filename that would itself fail validID (e.g. one
// somehow containing a separator) is skipped rather than trusted.
func readDirMemories(dir string) ([]memory.Memory, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("staging: read %q: %w", dir, err)
	}
	var out []memory.Memory
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		if err := validID(id); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		m, err := memory.Unmarshal(data)
		if err != nil {
			continue
		}
		m.ID = id
		out = append(out, m)
	}
	return out, nil
}
