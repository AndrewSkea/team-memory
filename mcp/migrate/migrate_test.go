package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/paths"
	"github.com/AndrewSkea/team-memory/mcp/staging"
	"github.com/AndrewSkea/team-memory/mcp/store"
)

func setup(t *testing.T) (*store.Store, *staging.Area, paths.Layout) {
	t.Helper()
	l, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatalf("paths.New: %v", err)
	}
	s, err := store.Open(l)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	a, err := staging.New(l)
	if err != nil {
		t.Fatalf("staging.New: %v", err)
	}
	return s, a, l
}

func writeV1(t *testing.T, l paths.Layout, name, content string) {
	t.Helper()
	p := filepath.Join(l.Clone, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write %q: %v", name, err)
	}
}

func TestRunConvertsEntries(t *testing.T) {
	s, a, l := setup(t)
	writeV1(t, l, "TOPIC.md", v1Doc)

	res, err := Run(s, a)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Converted != 2 {
		t.Errorf("Converted = %d, want 2", res.Converted)
	}

	ms, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(ms) != 2 {
		t.Fatalf("store holds %d memories, want 2", len(ms))
	}
}

func TestRunIsIdempotent(t *testing.T) {
	s, a, l := setup(t)
	writeV1(t, l, "TOPIC.md", v1Doc)

	if _, err := Run(s, a); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if _, err := Run(s, a); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	ms, _ := s.All()
	if len(ms) != 2 {
		t.Errorf("after two runs store holds %d memories, want 2", len(ms))
	}
}

func TestRunSendsUnsureToPending(t *testing.T) {
	s, a, l := setup(t)
	writeV1(t, l, "UNSURE.md", v1Doc)

	res, err := Run(s, a)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Pending != 2 {
		t.Errorf("Pending = %d, want 2", res.Pending)
	}
	if res.Converted != 0 {
		t.Errorf("Converted = %d, want 0 — UNSURE entries must not be promoted", res.Converted)
	}

	pending, err := a.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("staging holds %d pending, want 2", len(pending))
	}
}

// malformedDoc exercises every deliberately-broken shape the brief calls
// out: a truncated entry, an entry with no **Full:**, an entry that is
// nothing but a header, a well-formed entry (as a control), and an entry
// with an unparseable timestamp. Empty files and entry-free files are
// covered separately below, since splitBlocks reports zero blocks for both
// and there is nothing here to lose.
const malformedDoc = `### Entry: 2026-01-01T00:00:00Z — Truncated, no full section
**Scope:** shared
**Type:** decision
**Tags:** x

### Entry: 2026-01-02T00:00:00Z — Header and nothing else

### Entry: 2026-01-03T09:30:00Z — A perfectly good entry
**Scope:** shared
**Type:** decision
**Tags:** storage
**Source:** session-good
**Summary:** This one parses fine.
**Bullets:** - ok
**Full:**
Body text.

### Entry: not-a-timestamp — Unparseable timestamp
**Scope:** shared
**Full:**
body
`

// TestParseEntriesNeverDropsABlock is the hard requirement from the brief:
// every "### Entry:" block that goes in comes out as either a parsed memory
// or an Unparsed record. A block satisfying neither is a bug, not an
// acceptable skip.
func TestParseEntriesNeverDropsABlock(t *testing.T) {
	blocks := splitBlocks(malformedDoc)
	if len(blocks) != 4 {
		t.Fatalf("test fixture has %d blocks, want 4 — fix the fixture, not the assertion", len(blocks))
	}

	got, bad := ParseEntries("MALFORMED.md", malformedDoc)
	if len(got)+len(bad) != len(blocks) {
		t.Fatalf("in=%d out=%d(parsed)+%d(unparsed)=%d — an entry was dropped",
			len(blocks), len(got), len(bad), len(got)+len(bad))
	}
	if len(got) != 1 {
		t.Errorf("parsed = %d, want 1 (only the well-formed entry)", len(got))
	}
	if len(bad) != 3 {
		t.Errorf("unparsed = %d, want 3 (truncated, header-only, bad timestamp)", len(bad))
	}
	for _, u := range bad {
		if u.Raw == "" {
			t.Error("an Unparsed record has no Raw text — that block's content would be lost")
		}
		if u.Reason == "" {
			t.Error("an Unparsed record has no Reason")
		}
	}
}

// TestParseEntriesHandlesEmptyAndEntryFreeDocuments covers the two cases
// where the "in" count is legitimately zero: nothing is dropped because
// nothing was ever there.
func TestParseEntriesHandlesEmptyAndEntryFreeDocuments(t *testing.T) {
	for name, doc := range map[string]string{
		"empty file":        "",
		"no entries, prose": "# Just a heading\n\nSome prose with no entries at all.\n",
	} {
		got, bad := ParseEntries(name, doc)
		if len(got) != 0 || len(bad) != 0 {
			t.Errorf("%s: got %d parsed and %d unparsed, want 0 and 0", name, len(got), len(bad))
		}
	}
}

// TestRunNeverDropsAnEntry runs the full pipeline (not just the parser) over
// malformedDoc and checks that Converted+Unparsed accounts for every block,
// and that every unparsed block actually landed on disk under
// memories/team/unparsed/ — the store, not just the in-memory Result, must
// agree nothing was lost.
func TestRunNeverDropsAnEntry(t *testing.T) {
	s, a, l := setup(t)
	writeV1(t, l, "MALFORMED.md", malformedDoc)

	res, err := Run(s, a)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantTotal := len(splitBlocks(malformedDoc))
	gotTotal := res.Converted + res.Unparsed + res.Pending
	if gotTotal != wantTotal {
		t.Fatalf("Run accounted for %d of %d entries — the rest were dropped", gotTotal, wantTotal)
	}
	if res.Converted != 1 || res.Unparsed != 3 {
		t.Errorf("Converted=%d Unparsed=%d, want 1 and 3", res.Converted, res.Unparsed)
	}

	dir := filepath.Join(l.Clone, "memories", "team", "unparsed")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir unparsed: %v", err)
	}
	if len(entries) != res.Unparsed {
		t.Errorf("unparsed dir holds %d files, want %d to match Result.Unparsed", len(entries), res.Unparsed)
	}

	ms, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(ms) != res.Converted {
		t.Errorf("store holds %d memories, want %d to match Result.Converted", len(ms), res.Converted)
	}
}

// TestRunIsIdempotentWithUnparsedEntries runs Run twice over a document
// containing unparsable blocks and checks the unparsed directory does not
// accumulate duplicates on the second pass — preserve()'s filenames are
// content-hashed for exactly this reason.
func TestRunIsIdempotentWithUnparsedEntries(t *testing.T) {
	s, a, l := setup(t)
	writeV1(t, l, "MALFORMED.md", malformedDoc)

	first, err := Run(s, a)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	second, err := Run(s, a)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if second.Unparsed != first.Unparsed {
		t.Errorf("second run Unparsed = %d, want %d (re-running must not change the count)",
			second.Unparsed, first.Unparsed)
	}

	dir := filepath.Join(l.Clone, "memories", "team", "unparsed")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir unparsed: %v", err)
	}
	if len(entries) != first.Unparsed {
		t.Errorf("after two runs unparsed dir holds %d files, want %d — duplicates were written",
			len(entries), first.Unparsed)
	}

	ms, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(ms) != first.Converted {
		t.Errorf("after two runs store holds %d memories, want %d — duplicates were committed",
			len(ms), first.Converted)
	}
}

// duplicateEntry appears verbatim in two different v1 documents below. Same
// header, same fields, same body — so memory.ComputeID gives it the same id
// both times, and it must land on disk as exactly one file.
const duplicateEntry = "### Entry: 2026-01-01T00:00:00Z — Duplicated entry\n" +
	"**Scope:** shared\n**Type:** decision\n**Tags:** x\n" +
	"**Source:** session-dup\n**Summary:** same content twice.\n" +
	"**Bullets:** - only one\n**Full:**\nIdentical body text.\n"

// TestRunConvertedCountsDistinctMemoriesNotBlocks is fix #4 from the task-9
// review: two byte-identical v1 blocks are the same memory by definition
// (ComputeID hashes Body+Author+Created), so only one file lands on disk —
// Result.Converted must report 1, not 2, or the number shown to a user
// overstates what actually happened.
func TestRunConvertedCountsDistinctMemoriesNotBlocks(t *testing.T) {
	s, a, l := setup(t)
	writeV1(t, l, "TOPIC-A.md", duplicateEntry)
	writeV1(t, l, "TOPIC-B.md", duplicateEntry)

	res, err := Run(s, a)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Converted != 1 {
		t.Errorf("Converted = %d, want 1 — one file exists, so one memory converted", res.Converted)
	}

	ms, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(ms) != 1 {
		t.Errorf("store holds %d memories, want 1", len(ms))
	}
}

func TestRunPreservesUnparsableEntries(t *testing.T) {
	s, a, l := setup(t)
	writeV1(t, l, "BROKEN.md",
		"### Entry: not-a-timestamp — Broken\n**Scope:** shared\n**Full:**\nbody\n")

	res, err := Run(s, a)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Unparsed != 1 {
		t.Errorf("Unparsed = %d, want 1", res.Unparsed)
	}
	dir := filepath.Join(l.Clone, "memories", "team", "unparsed")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir unparsed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("unparsed dir holds %d files, want 1 — nothing may be dropped", len(entries))
	}
}

// TestRerunDoesNotResurrectRejectedEntries is the regression proof: Run
// re-Added every UNSURE.md entry on every run and never consulted the bin, so
// migrate -> reject -> migrate put the SAME id in pending and in the bin at
// once. A human's rejection was undone by re-running the migration.
func TestRerunDoesNotResurrectRejectedEntries(t *testing.T) {
	s, a, l := setup(t)
	writeV1(t, l, "UNSURE.md", v1Doc)

	if _, err := Run(s, a); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	pending, err := a.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) == 0 {
		t.Fatal("expected the first run to queue UNSURE entries")
	}
	for _, m := range pending {
		if err := a.Reject(m.ID, "not worth keeping"); err != nil {
			t.Fatalf("Reject %s: %v", m.ID, err)
		}
	}

	res, err := Run(s, a)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res.Pending != 0 {
		t.Errorf("second run reported %d newly queued entries, want 0", res.Pending)
	}

	pending, err = a.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("rejected entries came back into the queue: %d pending", len(pending))
	}
	binned, err := a.Binned()
	if err != nil {
		t.Fatalf("Binned: %v", err)
	}
	if len(binned) == 0 {
		t.Fatal("the bin should still hold the rejections")
	}
	for _, b := range binned {
		if b.Reason != "not worth keeping" {
			t.Errorf("rejection reason for %s was lost: %q", b.Memory.ID, b.Reason)
		}
	}
}
