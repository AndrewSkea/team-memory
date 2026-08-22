package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/go-git/go-git/v5/plumbing"
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

// TestRegenerateIndexOnUnchangedMemoriesIsNoop asserts that calling
// RegenerateIndex again with no memory changes since the last regeneration
// is a successful no-op: content is byte-identical, so there is nothing new
// to stage, commitPath's underlying commit legitimately reports
// git.ErrEmptyCommit, and commitPath treats a clean worktree as success
// rather than an error. It proves the no-op actually happened — HEAD does
// not move — not merely that the second call returned without error.
func TestRegenerateIndexOnUnchangedMemoriesIsNoop(t *testing.T) {
	l := newLayout(t)
	s, _ := Open(l)
	if _, err := s.Put(newMemory("Body."), "add"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.RegenerateIndex(); err != nil {
		t.Fatalf("RegenerateIndex (1st): %v", err)
	}

	headBefore, err := s.repo.Head()
	if err != nil {
		t.Fatalf("Head (before): %v", err)
	}

	hash, err := s.RegenerateIndex()
	if err != nil {
		t.Fatalf("RegenerateIndex (2nd, no changes) must succeed as a no-op, got: %v", err)
	}
	if hash != plumbing.ZeroHash {
		t.Errorf("RegenerateIndex (2nd, no changes) returned hash %s, want ZeroHash", hash)
	}

	headAfter, err := s.repo.Head()
	if err != nil {
		t.Fatalf("Head (after): %v", err)
	}
	if headBefore.Hash() != headAfter.Hash() {
		t.Errorf("HEAD moved on a no-op RegenerateIndex: %s -> %s", headBefore.Hash(), headAfter.Hash())
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

// renderedLines returns rendered split into its lines with the single
// trailing newline (every row ends in "\n") removed first, so callers get
// exactly one string per line with no spurious trailing empty element.
func renderedLines(t *testing.T, rendered string) []string {
	t.Helper()
	return strings.Split(strings.TrimSuffix(rendered, "\n"), "\n")
}

// tableRowFields splits one already-escaped table row on "|" and asserts it
// has exactly the 8 pieces a 6-column "| a | b | c | d | e | f |" row
// produces (an empty piece before the leading "|" and after the trailing
// "|", plus one piece per column). A hostile "|" that escaped correctly
// leaves the row's own column count unchanged; a "|" that leaked through
// unescaped opens a spurious cell and this count catches it directly.
// tableCellPlaceholder stands in for an escaped pipe ("\|") while counting
// cell boundaries, exactly as a real Markdown table parser treats it: not a
// delimiter. Splitting on raw "|" would flag every correctly escaped pipe as
// a spurious cell, which is the opposite of what this check is for.
const tableCellPlaceholder = "\x00ESCAPED-PIPE\x00"

func tableRowFields(t *testing.T, row string) []string {
	t.Helper()
	masked := strings.ReplaceAll(row, `\|`, tableCellPlaceholder)
	fields := strings.Split(masked, "|")
	if len(fields) != 8 {
		t.Fatalf("row %q has %d cell boundaries (unescaped \"|\"s), want 8 (a leaked \"|\" opened a spurious cell): %q", row, len(fields), fields)
	}
	for i := range fields {
		fields[i] = strings.ReplaceAll(fields[i], tableCellPlaceholder, `\|`)
	}
	return fields
}

func TestRenderIndexEscapesPipeInSlug(t *testing.T) {
	hostile := newMemory("Body.")
	hostile.Created = time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	hostile.Slug = "danger|ous"
	hostile.ID = memory.ComputeID(hostile)

	sentinel := newMemory("Sentinel body.")
	sentinel.Created = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	sentinel.Slug = "sentinel-row"
	sentinel.ID = memory.ComputeID(sentinel)

	rendered := RenderIndex([]memory.Memory{hostile, sentinel})
	lines := renderedLines(t, rendered)
	// 4 header lines ("# INDEX...", "", "Generated file...", "") + 2 table
	// header lines + one line per memory.
	wantLines := 6 + 2
	if len(lines) != wantLines {
		t.Fatalf("rendered has %d lines, want %d (a leaked \"|\" split a row): %q", len(lines), wantLines, lines)
	}

	hostileLine := lines[len(lines)-2]
	tableRowFields(t, hostileLine)
	if !strings.Contains(hostileLine, `danger\|ous`) {
		t.Errorf("hostile row does not contain the escaped slug: %q", hostileLine)
	}

	sentinelLine := lines[len(lines)-1]
	fields := tableRowFields(t, sentinelLine)
	if !strings.Contains(sentinelLine, sentinel.ID) || !strings.Contains(sentinelLine, "sentinel-row") {
		t.Errorf("sentinel row was deranged by the preceding hostile row: %q", sentinelLine)
	}
	_ = fields
}

func TestRenderIndexEscapesNewlineInSlug(t *testing.T) {
	hostile := newMemory("Body.")
	hostile.Created = time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	hostile.Slug = "line1\nline2"
	hostile.ID = memory.ComputeID(hostile)

	sentinel := newMemory("Sentinel body.")
	sentinel.Created = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	sentinel.Slug = "sentinel-row"
	sentinel.ID = memory.ComputeID(sentinel)

	rendered := RenderIndex([]memory.Memory{hostile, sentinel})
	lines := renderedLines(t, rendered)
	wantLines := 6 + 2
	if len(lines) != wantLines {
		t.Fatalf("rendered has %d lines, want %d (an embedded newline split a row): %q", len(lines), wantLines, lines)
	}

	hostileLine := lines[len(lines)-2]
	tableRowFields(t, hostileLine)
	if !strings.Contains(hostileLine, `line1\nline2`) {
		t.Errorf("hostile row does not contain the escaped newline: %q", hostileLine)
	}

	sentinelLine := lines[len(lines)-1]
	tableRowFields(t, sentinelLine)
	if !strings.Contains(sentinelLine, sentinel.ID) || !strings.Contains(sentinelLine, "sentinel-row") {
		t.Errorf("sentinel row was deranged by the preceding hostile row's embedded newline: %q", sentinelLine)
	}
}

func TestRenderIndexEscapesPipeAndNewlineInAuthor(t *testing.T) {
	hostile := newMemory("Body.")
	hostile.Created = time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	hostile.Author = "eve|attacker\ninjected row"
	hostile.ID = memory.ComputeID(hostile)

	sentinel := newMemory("Sentinel body.")
	sentinel.Created = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	sentinel.Slug = "sentinel-row"
	sentinel.ID = memory.ComputeID(sentinel)

	rendered := RenderIndex([]memory.Memory{hostile, sentinel})
	lines := renderedLines(t, rendered)
	wantLines := 6 + 2
	if len(lines) != wantLines {
		t.Fatalf("rendered has %d lines, want %d (hostile author split a row): %q", len(lines), wantLines, lines)
	}

	hostileLine := lines[len(lines)-2]
	tableRowFields(t, hostileLine)
	if !strings.Contains(hostileLine, `eve\|attacker\ninjected row`) {
		t.Errorf("hostile row does not contain the escaped author: %q", hostileLine)
	}

	sentinelLine := lines[len(lines)-1]
	tableRowFields(t, sentinelLine)
	if !strings.Contains(sentinelLine, sentinel.ID) || !strings.Contains(sentinelLine, "sentinel-row") {
		t.Errorf("sentinel row was deranged by the preceding hostile author: %q", sentinelLine)
	}
}

func TestRenderIndexEscapesPipeInTag(t *testing.T) {
	hostile := newMemory("Body.")
	hostile.Created = time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	hostile.Tags = []string{"good", "bad|tag"}
	hostile.ID = memory.ComputeID(hostile)

	sentinel := newMemory("Sentinel body.")
	sentinel.Created = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	sentinel.Slug = "sentinel-row"
	sentinel.ID = memory.ComputeID(sentinel)

	rendered := RenderIndex([]memory.Memory{hostile, sentinel})
	lines := renderedLines(t, rendered)
	wantLines := 6 + 2
	if len(lines) != wantLines {
		t.Fatalf("rendered has %d lines, want %d (hostile tag split a row): %q", len(lines), wantLines, lines)
	}

	hostileLine := lines[len(lines)-2]
	tableRowFields(t, hostileLine)
	if !strings.Contains(hostileLine, `bad\|tag`) {
		t.Errorf("hostile row does not contain the escaped tag: %q", hostileLine)
	}

	sentinelLine := lines[len(lines)-1]
	tableRowFields(t, sentinelLine)
	if !strings.Contains(sentinelLine, sentinel.ID) || !strings.Contains(sentinelLine, "sentinel-row") {
		t.Errorf("sentinel row was deranged by the preceding hostile tag: %q", sentinelLine)
	}
}

// TestRenderIndexEscapingIsDeterministic re-runs the byte-identity check
// with hostile values (pipes, newlines, backslashes) mixed into the set, so
// escaping cannot have introduced any nondeterminism (e.g. via map
// iteration in some future rewrite).
func TestRenderIndexEscapingIsDeterministic(t *testing.T) {
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	hostileValues := []string{
		"plain",
		"pipe|here",
		"newline\nhere",
		"crlf\r\nhere",
		`backslash\here`,
		`mixed\|\n"`,
	}
	var ms []memory.Memory
	for i, v := range hostileValues {
		m := newMemory("Body.")
		m.Created = base.Add(time.Duration(i) * time.Hour)
		m.Author = v
		m.Slug = v
		m.Tags = []string{v}
		m.ID = memory.ComputeID(m)
		ms = append(ms, m)
	}

	first := RenderIndex(ms)
	for i := 0; i < 5; i++ {
		if got := RenderIndex(ms); got != first {
			t.Fatalf("RenderIndex with hostile values produced different bytes on run %d", i)
		}
	}
}

// TestAllSkipsMemoriesWithATraversingID keeps All from handing a caller an ID
// that cannot legally name a file. All reads the ID out of file content and,
// unlike every staging reader, cannot re-derive it from the filename. A caller
// that fed such an ID back into RepoPath would rebuild the exact escape that
// staging.Take was fixed for, so the file is skipped at the source instead.
func TestAllSkipsMemoriesWithATraversingID(t *testing.T) {
	l := newLayout(t)
	s, _ := Open(l)

	dir := filepath.Join(l.Clone, "memories", "team", "2026", "08")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	for _, id := range []string{"../../../../evil", "..", `..\..\evil`, "c:evil", ".hidden"} {
		body := "---\nid: " + id + "\nscope: team\nauthor: andrew\n" +
			"created: 2026-08-22T10:00:00Z\nslug: a-thing\n---\n\nBody.\n"
		name := filepath.Join(dir, "aaaabbbbccccdddd-a-thing.md")
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatalf("write %q: %v", id, err)
		}

		got, err := s.All()
		if err != nil {
			t.Fatalf("All (%q): %v", id, err)
		}
		if len(got) != 0 {
			t.Errorf("All returned %d for id %q, want 0", len(got), id)
		}
		if err := os.Remove(name); err != nil {
			t.Fatalf("Remove: %v", err)
		}
	}

	// The guard must not reject a legitimate content-addressed ID.
	good := "---\nid: aaaabbbbccccdddd\nscope: team\nauthor: andrew\n" +
		"created: 2026-08-22T10:00:00Z\nslug: a-thing\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "aaaabbbbccccdddd-a-thing.md"), []byte(good), 0o600); err != nil {
		t.Fatalf("write good: %v", err)
	}
	got, err := s.All()
	if err != nil {
		t.Fatalf("All (good): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("All returned %d for a valid id, want 1", len(got))
	}
}
