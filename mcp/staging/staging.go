// Package staging holds memories that have not been approved.
//
// Everything here lives under the layout's Staging directory, which is a
// sibling of the git clone. Nothing in this package ever touches the clone.
//
// Approval is a two-step handshake, because this package is not the commit
// point - the store is. Take(id) CLAIMS a pending memory and returns it,
// leaving the file on disk; the caller commits it and then calls Done(id) to
// delete it, or Abort(id) to release the claim and leave it queued for a
// retry. Take must not delete, or a memory the store refuses (an unusable
// author, a disk error, a git error) would be destroyed with no compensating
// re-add anywhere.
//
// The claim is a zero-byte "<id>.md.taking" file beside the memory. If you
// are a human looking at one in a text editor: it means somebody is
// mid-approval of that memory, and Pending() therefore hides the memory
// while it exists. A marker whose mtime is more than staleClaimAge away from
// now belongs to no live caller and is ignored and
// reclaimed, so deleting one by hand is safe but rarely necessary.
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

// validID delegates to memory.ValidID so there is exactly ONE id validator
// in the codebase. Ids validated here become paths under Pending/Bin; the
// same ids validated in memory.RepoPath become paths inside the clone. Two
// validators would mean two chances to drift apart, and the weaker of the
// two would define the real guarantee.
func validID(id string) error {
	if err := memory.ValidID(id); err != nil {
		return fmt.Errorf("staging: %w", err)
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
	all, err := readDirMemories(a.layout.Pending)
	if err != nil {
		return nil, err
	}
	// An item with a live claim is mid-approval, not awaiting review: showing
	// it would invite a second reviewer to act on a memory another caller is
	// already committing. A STALE claim is a crashed caller's leftover and
	// must not hide the memory, or a crash mid-approval would drop it out of
	// the queue for good.
	ms := make([]memory.Memory, 0, len(all))
	for _, m := range all {
		if a.claimHeld(m.ID) {
			continue
		}
		ms = append(ms, m)
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

// staleClaimAge is far beyond any real Take: Take does one read, one parse,
// and one remove, which completes in milliseconds, not minutes. A claim
// marker older than this cannot belong to a live caller — it can only be
// left over from a process that crashed between creating the marker and
// removing it. Do not lower this "to make recovery snappier"; a threshold
// close to how long a real Take can take reintroduces the double-take that
// the marker exists to prevent, since a live caller's marker could then be
// mistaken for stale and reclaimed out from under it.
const staleClaimAge = time.Minute

// Take atomically claims a pending memory and returns it. It does NOT
// remove anything: the caller commits the memory and then calls Done to
// delete it, or Abort to release the claim and leave it queued. See the
// package doc for why the delete cannot live here.
//
// While the claim is held the memory is hidden from Pending, so a
// half-finished approval is not offered up for review a second time.
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

	lock, err := a.claimPending(claim)
	if err != nil {
		return memory.Memory{}, fmt.Errorf("staging: claim pending %s: %w", id, err)
	}
	lock.Close()

	m, err := a.readPending(id, p)
	if err != nil {
		// We failed before handing anything back, so nothing downstream can
		// call Done or Abort for us: release our own claim, or an unreadable
		// pending file (which a human may well be about to fix in place)
		// stays wedged until the marker goes stale.
		_ = os.Remove(claim)
		return memory.Memory{}, err
	}
	return m, nil
}

// readPending reads and parses one pending file, keying the memory's ID off
// the filename rather than the frontmatter.
func (a *Area) readPending(id, p string) (memory.Memory, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return memory.Memory{}, fmt.Errorf("staging: read pending %s: %w", id, err)
	}
	m, err := memory.Unmarshal(data)
	if err != nil {
		return memory.Memory{}, fmt.Errorf("staging: parse pending %s: %w", id, err)
	}
	// The frontmatter id is file content and therefore untrusted: a pending
	// file whose frontmatter claims "../../../../evil" would otherwise reach
	// memory.RepoPath and commit outside memories/. readDirMemories does the
	// same overwrite for the same reason; Take is the reader whose output
	// reaches the clone, so it must not be the one that skips it.
	m.ID = id
	return m, nil
}

// Done completes a Take: it removes the pending memory and releases the
// claim. Call it only once the store has actually committed the memory -
// this is the point of no return, and it is deliberately not inside Take.
func (a *Area) Done(id string) error {
	if err := validID(id); err != nil {
		return err
	}
	if err := os.Remove(a.pendingPath(id)); err != nil && !os.IsNotExist(err) {
		// Leave the claim in place: the memory is still there, and a
		// still-claimed item is safer than one that reappears in the review
		// queue while a caller believes it is committed.
		return fmt.Errorf("staging: remove pending %s: %w", id, err)
	}
	if err := os.Remove(a.pendingPath(id) + ".taking"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("staging: release claim %s: %w", id, err)
	}
	return nil
}

// Abort releases a claim without removing anything: the memory goes back
// into the review queue for a later retry. Call it when the commit failed.
func (a *Area) Abort(id string) error {
	if err := validID(id); err != nil {
		return err
	}
	if err := os.Remove(a.pendingPath(id) + ".taking"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("staging: release claim %s: %w", id, err)
	}
	return nil
}

// claimIsStale reports whether a claim marker with this mtime cannot belong
// to a live caller: a marker this old is the crashed-process case, left
// behind by a Take that never reached Done or Abort.
func claimIsStale(mod time.Time) bool {
	age := time.Since(mod)
	return age >= staleClaimAge
}

// claimHeld reports whether a live (non-stale) claim marker exists for a
// pending id. A missing or unreadable marker is reported as not held: the
// claim is an advisory hint for the review queue, and failing closed here
// would hide a memory from review rather than protect anything.
func (a *Area) claimHeld(id string) bool {
	info, err := os.Stat(a.pendingPath(id) + ".taking")
	if err != nil {
		return false
	}
	return !claimIsStale(info.ModTime())
}

func (a *Area) claimPending(claim string) (*os.File, error) {
	lock, err := os.OpenFile(claim, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		return lock, nil
	}
	if !os.IsExist(err) {
		return nil, err
	}

	info, statErr := os.Stat(claim)
	if statErr != nil {
		// The marker existed a moment ago (our O_EXCL open just failed
		// because of it) but is gone now — its owner finished and removed
		// it between our two calls. That's an ordinary race, not staleness;
		// retry once and return whatever that attempt reports.
		return os.OpenFile(claim, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	}
	if !claimIsStale(info.ModTime()) {
		return nil, err
	}

	_ = os.Remove(claim)
	return os.OpenFile(claim, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
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
