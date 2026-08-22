package staging

import (
	"os"
	"path/filepath"
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
