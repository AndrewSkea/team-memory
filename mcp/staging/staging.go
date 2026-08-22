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

// Add writes a candidate memory into pending.
func (a *Area) Add(m memory.Memory) error {
	if err := validID(m.ID); err != nil {
		return err
	}
	data, err := memory.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(a.pendingPath(m.ID), data, 0o600); err != nil {
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

// Take reads a pending memory and removes it from pending. Used when a memory
// is approved and about to be committed.
func (a *Area) Take(id string) (memory.Memory, error) {
	if err := validID(id); err != nil {
		return memory.Memory{}, err
	}
	p := a.pendingPath(id)
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
func (a *Area) Reject(id, reason string) error {
	if err := validID(id); err != nil {
		return err
	}
	if err := os.Rename(a.pendingPath(id), a.binPath(id)); err != nil {
		return fmt.Errorf("staging: move %s to bin: %w", id, err)
	}
	meta, err := json.Marshal(rejection{Reason: reason, RejectedAt: time.Now().UTC()})
	if err != nil {
		return fmt.Errorf("staging: encode rejection for %s: %w", id, err)
	}
	if err := os.WriteFile(a.binMeta(id), meta, 0o600); err != nil {
		return fmt.Errorf("staging: write rejection for %s: %w", id, err)
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

// Binned lists rejected memories with their reasons.
func (a *Area) Binned() ([]BinItem, error) {
	ms, err := readDirMemories(a.layout.Bin)
	if err != nil {
		return nil, err
	}
	out := make([]BinItem, 0, len(ms))
	for _, m := range ms {
		item := BinItem{Memory: m}
		if raw, err := os.ReadFile(a.binMeta(m.ID)); err == nil {
			var r rejection
			if json.Unmarshal(raw, &r) == nil {
				item.Reason = r.Reason
				item.RejectedAt = r.RejectedAt
			}
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RejectedAt.After(out[j].RejectedAt)
	})
	return out, nil
}

// readDirMemories loads every .md file in a directory, skipping unparsable
// ones so a single bad file never hides the rest of the queue.
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
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		m, err := memory.Unmarshal(data)
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}
