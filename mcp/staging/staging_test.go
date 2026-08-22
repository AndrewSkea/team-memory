package staging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/paths"
)

func newArea(t *testing.T) (*Area, paths.Layout) {
	t.Helper()
	l, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatalf("paths.New: %v", err)
	}
	a, err := New(l)
	if err != nil {
		t.Fatalf("staging.New: %v", err)
	}
	return a, l
}

func mem(id, body string) memory.Memory {
	return memory.Memory{
		ID: id, Slug: "a-thing", Scope: memory.ScopeTeam,
		Created: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC),
		Author:  "andrew", SourceSession: "sess-1", Body: body,
	}
}

func TestAddThenPending(t *testing.T) {
	a, _ := newArea(t)
	if err := a.Add(mem("aaaaaaaaaaaaaaaa", "First.")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, err := a.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(got) != 1 || got[0].Body != "First." {
		t.Fatalf("Pending = %+v, want one memory with body First.", got)
	}
}

func TestPendingFilesLandInStagingNotClone(t *testing.T) {
	a, l := newArea(t)
	if err := a.Add(mem("bbbbbbbbbbbbbbbb", "Unreviewed.")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	entries, err := os.ReadDir(l.Pending)
	if err != nil {
		t.Fatalf("ReadDir pending: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file in pending, got %d", len(entries))
	}
	// The clone must be untouched.
	if _, err := os.Stat(filepath.Join(l.Clone, "memories")); !os.IsNotExist(err) {
		t.Errorf("pending content must not create anything under the clone; stat err = %v", err)
	}
}

func TestRejectMovesToBinWithReason(t *testing.T) {
	a, _ := newArea(t)
	_ = a.Add(mem("cccccccccccccccc", "Wrong."))
	if err := a.Reject("cccccccccccccccc", "not a team fact"); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	pending, _ := a.Pending()
	if len(pending) != 0 {
		t.Errorf("pending should be empty after reject, got %d", len(pending))
	}
	binned, err := a.Binned()
	if err != nil {
		t.Fatalf("Binned: %v", err)
	}
	if len(binned) != 1 {
		t.Fatalf("bin should hold 1 item, got %d", len(binned))
	}
	if binned[0].Reason != "not a team fact" {
		t.Errorf("Reason = %q, want %q", binned[0].Reason, "not a team fact")
	}
}

func TestRestoreMovesBackToPending(t *testing.T) {
	a, _ := newArea(t)
	_ = a.Add(mem("dddddddddddddddd", "Reconsidered."))
	_ = a.Reject("dddddddddddddddd", "hasty")

	if err := a.Restore("dddddddddddddddd"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	pending, _ := a.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending should hold 1 after restore, got %d", len(pending))
	}
	binned, _ := a.Binned()
	if len(binned) != 0 {
		t.Errorf("bin should be empty after restore, got %d", len(binned))
	}
}

func TestTakeRemovesFromPending(t *testing.T) {
	a, _ := newArea(t)
	_ = a.Add(mem("eeeeeeeeeeeeeeee", "Approved."))

	m, err := a.Take("eeeeeeeeeeeeeeee")
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if m.Body != "Approved." {
		t.Errorf("Body = %q, want %q", m.Body, "Approved.")
	}
	pending, _ := a.Pending()
	if len(pending) != 0 {
		t.Errorf("pending should be empty after Take, got %d", len(pending))
	}
}

func TestTakeUnknownIDErrors(t *testing.T) {
	a, _ := newArea(t)
	if _, err := a.Take("ffffffffffffffff"); err == nil {
		t.Fatal("expected error taking an unknown id")
	}
}

func TestRejectRejectsIDWithSeparators(t *testing.T) {
	a, _ := newArea(t)
	if err := a.Reject("../../clone/INDEX", "traversal"); err == nil {
		t.Fatal("expected error for id containing path separators")
	}
}

// TestRejectIDCannotEscapeStaging proves that a traversal id cannot cause
// Reject to touch anything outside Pending/Bin. It plants a real secret file
// next to the staging tree (as a sibling of Clone) and asserts that after the
// rejected call the secret is untouched and does not appear as a bin entry.
func TestRejectIDCannotEscapeStaging(t *testing.T) {
	a, l := newArea(t)

	// Pending and Bin are both two path components below Home
	// (Home/staging/pending, Home/staging/bin), so an id of
	// "../../clone/secret" is exactly the traversal that would land back in
	// the clone from either directory.
	secret := filepath.Join(l.Clone, "secret.md")
	if err := os.WriteFile(secret, []byte("---\nid: zzzzzzzzzzzzzzzz\ncreated: 2026-08-22T00:00:00Z\n---\n\nSecret.\n"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	if err := a.Reject("../../clone/secret", "traversal"); err == nil {
		t.Fatal("expected error rejecting an id that attempts to escape staging via ../../clone/secret")
	}

	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("secret file must survive untouched: %v", err)
	}
	// The clone must gain no new file at all — not the secret being moved,
	// and not even a rejection sidecar landing next to it.
	escapedMeta := filepath.Join(l.Clone, "secret.json")
	if _, err := os.Stat(escapedMeta); !os.IsNotExist(err) {
		t.Errorf("Reject must not write anything into the clone via a traversal id; stat err = %v", err)
	}
	binned, err := a.Binned()
	if err != nil {
		t.Fatalf("Binned: %v", err)
	}
	if len(binned) != 0 {
		t.Errorf("bin must not gain an entry from a traversal id, got %d", len(binned))
	}
}

// TestValidIDRejectsDriveLetter proves ids that look like an absolute
// Windows path (drive letter) are refused, not silently joined.
func TestValidIDRejectsDriveLetter(t *testing.T) {
	if err := validID(`C:\Windows\system32`); err == nil {
		t.Fatal("expected error for id containing a drive letter")
	}
}

// TestValidIDRejectsNUL proves an id containing a NUL byte is refused.
func TestValidIDRejectsNUL(t *testing.T) {
	if err := validID("aa\x00bb"); err == nil {
		t.Fatal("expected error for id containing a NUL byte")
	}
}

func TestPendingOrderedBySessionThenCreatedThenID(t *testing.T) {
	a, _ := newArea(t)
	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)

	m1 := mem("1111111111111111", "one")
	m1.SourceSession = "sess-b"
	m1.Created = base

	m2 := mem("2222222222222222", "two")
	m2.SourceSession = "sess-a"
	m2.Created = base

	// Same session and created time as m2, but a lexically greater ID —
	// this proves ID is used as the final tiebreak for a total order.
	m3 := mem("0000000000000000", "three")
	m3.SourceSession = "sess-a"
	m3.Created = base

	for _, m := range []memory.Memory{m1, m2, m3} {
		if err := a.Add(m); err != nil {
			t.Fatalf("Add %s: %v", m.ID, err)
		}
	}

	got, err := a.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 pending, got %d", len(got))
	}
	wantOrder := []string{"0000000000000000", "2222222222222222", "1111111111111111"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("position %d: ID = %q, want %q", i, got[i].ID, want)
		}
	}
}

// TestPendingSkipsUnparsableFile proves one corrupt pending file does not
// hide the rest of the queue from the reviewer.
func TestPendingSkipsUnparsableFile(t *testing.T) {
	a, l := newArea(t)
	if err := a.Add(mem("3333333333333333", "Good.")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	garbage := filepath.Join(l.Pending, "corrupt.md")
	if err := os.WriteFile(garbage, []byte("not frontmatter at all"), 0o600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}

	got, err := a.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(got) != 1 || got[0].ID != "3333333333333333" {
		t.Fatalf("Pending = %+v, want only the good memory", got)
	}
}

// TestBinnedNeverReadsOutsideBin is the review-mandated proof that Binned
// keys its sidecar lookup off the filename readDirMemories saw, never off
// frontmatter content. It plants a bin file whose frontmatter id points
// outside Bin (at a "secret" sidecar sitting in the clone) and asserts
// Binned neither reads nor reports anything from that outside location.
//
// This is the test that actually carries the escape-proof weight for
// Binned; TestRejectRejectsIDWithSeparators (kept because the brief
// mandates it verbatim) is vacuous on its own — it still passes even with
// validID's separator/".." checks mutated out, because the rename then
// simply fails with ENOENT for an unrelated reason. This test, and
// TestRejectIDCannotEscapeStaging above, are the ones that actually
// exercise the guard.
func TestBinnedNeverReadsOutsideBin(t *testing.T) {
	a, l := newArea(t)

	// A "secret" sidecar sitting in the clone, wholly outside Bin, that must
	// never be surfaced through Binned as if it were a legitimate reason.
	secretJSON := filepath.Join(l.Clone, "secret.json")
	if err := os.WriteFile(secretJSON, []byte(`{"reason":"LEAKED SECRET","rejected_at":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatalf("write secret sidecar: %v", err)
	}

	// A bin entry whose frontmatter id attempts to walk out to that secret.
	planted := "---\nid: ../../clone/secret\ncreated: 2026-08-22T00:00:00Z\n---\n\nPlanted.\n"
	if err := os.WriteFile(filepath.Join(l.Bin, "innocent.md"), []byte(planted), 0o600); err != nil {
		t.Fatalf("plant bin file: %v", err)
	}

	binned, err := a.Binned()
	if err != nil {
		t.Fatalf("Binned: %v", err)
	}
	if len(binned) != 1 {
		t.Fatalf("expected 1 bin item, got %d", len(binned))
	}
	if binned[0].Reason == "LEAKED SECRET" {
		t.Fatal("Binned read the sidecar via a frontmatter-controlled id instead of the filename")
	}
	if binned[0].Reason != "" {
		t.Errorf("Reason = %q, want empty (no sidecar exists at the filename-derived key)", binned[0].Reason)
	}
	if binned[0].Memory.ID != "innocent" {
		t.Errorf("Memory.ID = %q, want filename-derived %q", binned[0].Memory.ID, "innocent")
	}

	// The id Binned reports must actually be usable by Restore — proving the
	// correctness half of the bug: a filename/frontmatter-id mismatch used
	// to produce a listed item that could not be restored.
	if err := a.Restore(binned[0].Memory.ID); err != nil {
		t.Fatalf("Restore using the ID Binned reported should work: %v", err)
	}
}

// TestBinnedOrderedByRejectedAtThenID is an end-to-end sanity check that a
// real Binned() call, through Reject, produces the wanted order for a small
// set of distinct rejection times. It does not by itself prove the ID
// tiebreak matters (see TestSortBinnedOrdersByRejectedAtThenID below for
// that) because os.ReadDir already hands readDirMemories entries in
// filename order, which is identical to ID order here.
func TestBinnedOrderedByRejectedAtThenID(t *testing.T) {
	a, l := newArea(t)
	same := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	ids := []string{"bbbbbbbbbbbbbbbb", "aaaaaaaaaaaaaaaa", "cccccccccccccccc"}

	for _, id := range ids {
		data, err := memory.Marshal(mem(id, "x"))
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if err := os.WriteFile(filepath.Join(l.Bin, id+".md"), data, 0o600); err != nil {
			t.Fatalf("write bin md %s: %v", id, err)
		}
		meta, err := json.Marshal(struct {
			Reason     string    `json:"reason"`
			RejectedAt time.Time `json:"rejected_at"`
		}{"r", same})
		if err != nil {
			t.Fatalf("marshal meta: %v", err)
		}
		if err := os.WriteFile(filepath.Join(l.Bin, id+".json"), meta, 0o600); err != nil {
			t.Fatalf("write bin json %s: %v", id, err)
		}
	}

	binned, err := a.Binned()
	if err != nil {
		t.Fatalf("Binned: %v", err)
	}
	if len(binned) != 3 {
		t.Fatalf("expected 3 bin items, got %d", len(binned))
	}
	wantOrder := []string{"aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb", "cccccccccccccccc"}
	for i, want := range wantOrder {
		if binned[i].Memory.ID != want {
			t.Errorf("position %d: ID = %q, want %q", i, binned[i].Memory.ID, want)
		}
	}
}

// TestSortBinnedOrdersByRejectedAtThenID is the test that actually carries
// the ID-tiebreak proof. It calls sortBinned directly on a slice that is
// deliberately NOT already in ID order (c, a, b — not a, b, c), so a sort
// with no tiebreak (which, for tied RejectedAt values, may leave input
// order untouched rather than reordering it) is caught rather than
// accidentally passing because the input happened to already be sorted.
func TestSortBinnedOrdersByRejectedAtThenID(t *testing.T) {
	same := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	items := []BinItem{
		{Memory: memory.Memory{ID: "cccccccccccccccc"}, RejectedAt: same},
		{Memory: memory.Memory{ID: "aaaaaaaaaaaaaaaa"}, RejectedAt: same},
		{Memory: memory.Memory{ID: "bbbbbbbbbbbbbbbb"}, RejectedAt: same},
	}

	sortBinned(items)

	wantOrder := []string{"aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb", "cccccccccccccccc"}
	for i, want := range wantOrder {
		if items[i].Memory.ID != want {
			t.Errorf("position %d: ID = %q, want %q", i, items[i].Memory.ID, want)
		}
	}
}

// TestAddIsAtomicNoTempFileLeftBehind checks that Add's atomic-write path
// leaves exactly the final file behind, with no temp file remnant.
func TestAddIsAtomicNoTempFileLeftBehind(t *testing.T) {
	a, l := newArea(t)
	if err := a.Add(mem("4444444444444444", "Atomic.")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	entries, err := os.ReadDir(l.Pending)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "4444444444444444.md" {
		t.Fatalf("Pending dir = %v, want exactly one file 4444444444444444.md", entries)
	}
}

// TestAddNeverExposesAPartialFile is the concurrency proof behind
// atomicity: readers hammering the final path throughout the entire Add
// call must only ever see "not there yet" or "fully written, parseable" —
// never a file that exists but is empty or truncated. A large body and
// several rounds widen the window in which a non-atomic write (open,
// truncate, then write) would let a concurrent reader observe a
// zero-length or partial file in between; several concurrent readers and
// repeated rounds raise the odds of a reader's poll landing inside that
// window when it exists.
func TestAddNeverExposesAPartialFile(t *testing.T) {
	const rounds = 10
	const readers = 4
	body := strings.Repeat("x", 16*1024*1024) // 16MiB widens the write window

	for round := 0; round < rounds; round++ {
		a, l := newArea(t)
		id := "6666666666666666"
		target := filepath.Join(l.Pending, id+".md")

		stop := make(chan struct{})
		var wg sync.WaitGroup
		var sawPartial int32
		for r := 0; r < readers; r++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					data, err := os.ReadFile(target)
					if err != nil {
						continue // not there yet — fine
					}
					if _, perr := memory.Unmarshal(data); perr != nil {
						atomic.StoreInt32(&sawPartial, 1)
						return
					}
				}
			}()
		}

		if err := a.Add(mem(id, body)); err != nil {
			t.Fatalf("round %d: Add: %v", round, err)
		}
		close(stop)
		wg.Wait()

		if atomic.LoadInt32(&sawPartial) == 1 {
			t.Fatalf("round %d: a concurrent reader observed a partial/unparsable pending file mid-write", round)
		}
	}
}

// TestRejectUnknownIDLeavesNoOrphanSidecar proves that when the memory-file
// move half of Reject fails, the sidecar written first is cleaned up rather
// than left behind with nothing to attach to.
func TestRejectUnknownIDLeavesNoOrphanSidecar(t *testing.T) {
	a, l := newArea(t)
	if err := a.Reject("9999999999999999", "no such pending memory"); err == nil {
		t.Fatal("expected error rejecting an id with no pending memory")
	}
	if _, err := os.Stat(filepath.Join(l.Bin, "9999999999999999.json")); !os.IsNotExist(err) {
		t.Errorf("sidecar must not remain after a failed reject; stat err = %v", err)
	}
}

// TestTakeIsAtomicConcurrentCallersDontDoubleTake proves two concurrent
// Take calls on the same id cannot both succeed — the atomic claim rename
// means exactly one caller gets the memory and the other gets an error.
func TestTakeIsAtomicConcurrentCallersDontDoubleTake(t *testing.T) {
	a, _ := newArea(t)
	if err := a.Add(mem("8888888888888888", "Racy.")); err != nil {
		t.Fatalf("Add: %v", err)
	}

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := a.Take("8888888888888888")
			errs[i] = err
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly 1 successful Take among %d concurrent callers, got %d", n, successes)
	}
}

// TestRejectSidecarNeverLagsRename is the race proof behind fix 4: the
// rejection sidecar must be written before the memory is moved into Bin, so
// a reader racing the two steps never observes the memory already listed
// in Bin with no sidecar behind it yet (which Binned() would report as a
// spuriously-empty Reason, indistinguishable from a rejection given no
// reason at all). Several pollers hit the bin path and sidecar path
// directly (bypassing Binned()'s own readdir overhead, which would
// otherwise mask the tiny window) throughout many rounds of Reject, and we
// fail if the memory is ever seen present in Bin before its sidecar is.
func TestRejectSidecarNeverLagsRename(t *testing.T) {
	const rounds = 50
	const pollers = 8
	const reason = "definitely not empty"

	for round := 0; round < rounds; round++ {
		a, l := newArea(t)
		id := "7777777777777777"
		if err := a.Add(mem(id, "body")); err != nil {
			t.Fatalf("round %d: Add: %v", round, err)
		}
		binFile := filepath.Join(l.Bin, id+".md")
		sidecarFile := filepath.Join(l.Bin, id+".json")

		stop := make(chan struct{})
		var wg sync.WaitGroup
		var sawUnbacked int32
		for p := 0; p < pollers; p++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					if _, err := os.Stat(binFile); err == nil {
						if _, err := os.Stat(sidecarFile); err != nil {
							atomic.StoreInt32(&sawUnbacked, 1)
							return
						}
					}
				}
			}()
		}

		if err := a.Reject(id, reason); err != nil {
			t.Fatalf("round %d: Reject: %v", round, err)
		}
		close(stop)
		wg.Wait()

		if atomic.LoadInt32(&sawUnbacked) == 1 {
			t.Fatalf("round %d: memory was present in Bin before its rejection sidecar existed", round)
		}
	}
}
