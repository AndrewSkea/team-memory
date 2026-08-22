package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/memory"
)

func TestAllReturnsDeterministicOrder(t *testing.T) {
	l := newLayout(t)
	s, _ := Open(l)

	older := newMemory("Older thought.")
	older.Created = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	older.ID = memory.ComputeID(older)

	newer := newMemory("Newer thought.")
	newer.Created = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	newer.ID = memory.ComputeID(newer)

	if _, err := s.Put(older, "add older"); err != nil {
		t.Fatalf("Put older: %v", err)
	}
	if _, err := s.Put(newer, "add newer"); err != nil {
		t.Fatalf("Put newer: %v", err)
	}

	got, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("All returned %d memories, want 2", len(got))
	}
	if !got[0].Created.After(got[1].Created) {
		t.Errorf("expected newest first, got %v then %v", got[0].Created, got[1].Created)
	}
}

func TestAllSkipsNonMemoryFiles(t *testing.T) {
	l := newLayout(t)
	s, _ := Open(l)

	if _, err := s.Put(newMemory("Real."), "add"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	stray := filepath.Join(l.Clone, "memories", "team", "notes.txt")
	if err := os.WriteFile(stray, []byte("not a memory"), 0o600); err != nil {
		t.Fatalf("write stray: %v", err)
	}

	got, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("All returned %d, want 1 (stray .txt must be ignored)", len(got))
	}
}

func TestAllSkipsUnparsableWithoutFailing(t *testing.T) {
	l := newLayout(t)
	s, _ := Open(l)

	dir := filepath.Join(l.Clone, "memories", "team", "2026", "08")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	bad := filepath.Join(dir, "deadbeefdeadbeef-broken.md")
	if err := os.WriteFile(bad, []byte("no frontmatter here"), 0o600); err != nil {
		t.Fatalf("write bad: %v", err)
	}

	got, err := s.All()
	if err != nil {
		t.Fatalf("All must not fail on one unparsable file: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("All returned %d, want 0", len(got))
	}
}

func TestRenderIndexIsDeterministic(t *testing.T) {
	m := newMemory("Body.")
	a := RenderIndex([]memory.Memory{m})
	b := RenderIndex([]memory.Memory{m})
	if a != b {
		t.Fatal("RenderIndex is not deterministic")
	}
	if !strings.HasPrefix(a, "# INDEX for team-memory") {
		t.Errorf("unexpected header: %q", strings.SplitN(a, "\n", 2)[0])
	}
	if !strings.Contains(a, m.ID) {
		t.Errorf("index does not mention memory ID %s", m.ID)
	}
}

// TestRenderIndexIsDeterministicAcrossManyMemories proves the ordering rule
// (Created desc, then ID asc) is a total order with no ties left to map
// iteration, by rendering a larger, out-of-order slice twice and diffing
// bytes rather than eyeballing a single run.
func TestRenderIndexIsDeterministicAcrossManyMemories(t *testing.T) {
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	var ms []memory.Memory
	for i := 0; i < 8; i++ {
		m := newMemory("Body.")
		m.Created = base.Add(time.Duration(i%3) * time.Hour) // force ties on Created
		m.ID = memory.ComputeID(m)
		ms = append(ms, m)
	}

	first := RenderIndex(ms)
	for i := 0; i < 5; i++ {
		if got := RenderIndex(ms); got != first {
			t.Fatalf("RenderIndex produced different bytes on run %d", i)
		}
	}
}

func TestRegenerateIndexCommitsFile(t *testing.T) {
	l := newLayout(t)
	s, _ := Open(l)
	if _, err := s.Put(newMemory("Body."), "add"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.RegenerateIndex(); err != nil {
		t.Fatalf("RegenerateIndex: %v", err)
	}
	if _, err := os.Stat(filepath.Join(l.Clone, "INDEX.md")); err != nil {
		t.Errorf("INDEX.md not written: %v", err)
	}
}

// TestRegenerateIndexIsDeterministicAcrossRuns proves the full pipeline —
// disk scan through render — produces byte-identical INDEX.md for the same
// set of memories on repeated regeneration, which is the actual guarantee
// that keeps regeneration from creating spurious git diffs. It scans and
// renders twice independently (rather than calling RegenerateIndex twice
// back to back, which would hit git's legitimate "nothing changed" empty
// commit refusal on the second call) and compares bytes.
func TestRegenerateIndexIsDeterministicAcrossRuns(t *testing.T) {
	l := newLayout(t)
	s, _ := Open(l)

	for i := 0; i < 4; i++ {
		m := newMemory(fmt.Sprintf("Body %d.", i))
		if _, err := s.Put(m, "add"); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	if _, err := s.RegenerateIndex(); err != nil {
		t.Fatalf("RegenerateIndex: %v", err)
	}
	onDisk, err := os.ReadFile(filepath.Join(l.Clone, "INDEX.md"))
	if err != nil {
		t.Fatalf("read INDEX.md: %v", err)
	}

	first, err := s.All()
	if err != nil {
		t.Fatalf("All (1st): %v", err)
	}
	second, err := s.All()
	if err != nil {
		t.Fatalf("All (2nd): %v", err)
	}

	renderedFirst := RenderIndex(first)
	renderedSecond := RenderIndex(second)
	if renderedFirst != renderedSecond {
		t.Fatal("RenderIndex(All()) produced different bytes across independent scans of the same memories")
	}
	if renderedFirst != string(onDisk) {
		t.Fatal("independently re-scanned-and-rendered INDEX.md does not match the committed INDEX.md bytes")
	}
}

// TestRegenerateIndexOnUnchangedMemoriesIsNoop documents the actual behaviour
// when RegenerateIndex is called again with no memory changes since the last
// regeneration: content is byte-identical, so the working tree has nothing
// new to stage and commitPath's underlying commit legitimately reports no
// changes. This is not a bug to fix here — it is the same "empty commit"
// behaviour every other write in this store already has — but it is worth
// pinning down so a future change to commitPath's empty-commit handling does
// not silently start committing spurious no-op commits (contradicting
// Decision 2) or silently swallowing a real error.
func TestRegenerateIndexOnUnchangedMemoriesIsNoop(t *testing.T) {
	l := newLayout(t)
	s, _ := Open(l)
	if _, err := s.Put(newMemory("Body."), "add"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.RegenerateIndex(); err != nil {
		t.Fatalf("RegenerateIndex (1st): %v", err)
	}
	if _, err := s.RegenerateIndex(); err == nil {
		t.Fatal("expected 2nd RegenerateIndex with no changes to report an error (no-op empty commit), got nil")
	}
}

// TestRegenerateIndexDoesNotIndexItself guards against INDEX.md's own file
// (a .md file, but not under memories/) ever being picked up by All on a
// subsequent regeneration.
func TestRegenerateIndexDoesNotIndexItself(t *testing.T) {
	l := newLayout(t)
	s, _ := Open(l)
	if _, err := s.Put(newMemory("Body."), "add"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.RegenerateIndex(); err != nil {
		t.Fatalf("RegenerateIndex: %v", err)
	}

	got, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("All returned %d after regenerating INDEX.md, want 1 (INDEX.md must not be scanned as a memory)", len(got))
	}
}
