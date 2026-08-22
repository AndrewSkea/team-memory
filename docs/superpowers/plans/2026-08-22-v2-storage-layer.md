# team-memory v2 — Storage Layer Implementation Plan (Plan 1 of 5)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the v2 storage layer — content-addressed one-file-per-memory over a local git clone, a rebuildable full-text index, a structurally unreachable staging area, and an idempotent migration from v1 Markdown entries.

**Architecture:** Memories become individual Markdown files with YAML frontmatter, named by a content hash so two writers never touch the same path and concurrent additions merge with no conflict handling. A local `go-git` clone is the read path (instant, offline, no rate limit). `INDEX.md` stops being authoritative state and becomes a deterministically regenerated artifact. Unreviewed content lives in `staging/`, a **sibling of** the clone directory, so it is unreachable by any worktree add pattern — enforced by a path-prefix guard and a permanent regression test.

**Tech Stack:** Go 1.26.2, `CGO_ENABLED=0`. `github.com/go-git/go-git/v5` v5.19.2. `modernc.org/sqlite` v1.57.0 with FTS5. `gopkg.in/yaml.v3` for frontmatter. Standard library for everything else.

**Spec:** `docs/superpowers/specs/2026-08-22-team-memory-v2-design.md` — this plan implements §5 (Storage) and §10.2 (Data migration). Read the spec alongside this plan.

## Global Constraints

- Go module is `github.com/AndrewSkea/team-memory/mcp`. New packages go under `mcp/`. Do **not** rename the module in this plan.
- `CGO_ENABLED=0` for every build and every test. No cgo dependency may enter the module.
- `github.com/go-git/go-git/v5` **v5.19.2 or later** — v5.19.1 and earlier carry symlink and path-traversal advisories.
- `modernc.org/sqlite` **v1.57.0**. FTS5 is available with **no build tag** — verified by compiling and running `CREATE VIRTUAL TABLE … USING fts5` plus a `MATCH` query under `CGO_ENABLED=0`.
- Git operations: fetch plus **fast-forward-only** merge. Never force-push. Never call `gc`. Never rewrite history.
- Default scope for any ambiguous memory is `team`, never `personal`.
- No file under `staging/` may ever be staged or committed to the git worktree. This is the load-bearing invariant of the whole product.
- Every test uses `t.TempDir()`. No test may touch the user's real `$TEAM_MEMORY_HOME`, real `~/.claude`, or the network.
- Run tests with `cd mcp && CGO_ENABLED=0 go test ./...`.

---

### Task 1: Memory type and frontmatter round-trip

**Files:**
- Create: `mcp/memory/memory.go`
- Test: `mcp/memory/memory_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Scope string`, constants `ScopeTeam Scope = "team"`, `ScopePersonal Scope = "personal"`
  - `type Memory struct { ID, Author, Slug, Body string; Scope Scope; Created time.Time; Tags []string; SourceSession string; Confidence float64 }`
  - `func Marshal(m Memory) ([]byte, error)`
  - `func Unmarshal(data []byte) (Memory, error)`

- [ ] **Step 1: Add the yaml dependency**

```bash
cd mcp && GOFLAGS=-mod=mod go get gopkg.in/yaml.v3@v3.0.1
```

- [ ] **Step 2: Write the failing test**

Create `mcp/memory/memory_test.go`:

```go
package memory

import (
	"strings"
	"testing"
	"time"
)

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	in := Memory{
		ID:            "a1b2c3d4e5f60718",
		Created:       time.Date(2026, 8, 22, 14, 30, 0, 0, time.UTC),
		Author:        "andrew",
		Scope:         ScopeTeam,
		Tags:          []string{"storage", "git"},
		SourceSession: "sess-123",
		Confidence:    0.8,
		Slug:          "chose-go-git-over-trees-api",
		Body:          "We picked go-git because reads are local.\n\nNo rate limit.",
	}

	data, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.HasPrefix(string(data), "---\n") {
		t.Fatalf("expected frontmatter delimiter, got %q", string(data)[:4])
	}

	out, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.ID != in.ID || out.Author != in.Author || out.Scope != in.Scope {
		t.Errorf("scalar mismatch: got %+v want %+v", out, in)
	}
	if !out.Created.Equal(in.Created) {
		t.Errorf("Created = %v, want %v", out.Created, in.Created)
	}
	if len(out.Tags) != 2 || out.Tags[0] != "storage" || out.Tags[1] != "git" {
		t.Errorf("Tags = %v, want [storage git]", out.Tags)
	}
	if out.Body != in.Body {
		t.Errorf("Body = %q, want %q", out.Body, in.Body)
	}
}

func TestUnmarshalRejectsMissingFrontmatter(t *testing.T) {
	_, err := Unmarshal([]byte("just a body with no frontmatter"))
	if err == nil {
		t.Fatal("expected error for missing frontmatter, got nil")
	}
}

func TestUnmarshalDefaultsScopeToTeam(t *testing.T) {
	data := []byte("---\nid: abc\ncreated: 2026-08-22T00:00:00Z\n---\nbody\n")
	m, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m.Scope != ScopeTeam {
		t.Errorf("Scope = %q, want %q", m.Scope, ScopeTeam)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./memory/ -run TestMarshal -v`
Expected: FAIL — build error, `undefined: Memory`.

- [ ] **Step 4: Write the implementation**

Create `mcp/memory/memory.go`:

```go
// Package memory defines the on-disk representation of a single memory:
// YAML frontmatter followed by a Markdown body.
package memory

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Scope decides which directory a memory is filed under. A personal memory
// still lives in the shared repository under memories/people/<author>/ — it is
// filed differently, not concealed.
type Scope string

const (
	ScopeTeam     Scope = "team"
	ScopePersonal Scope = "personal"
)

var delimiter = []byte("---\n")

// ErrNoFrontmatter is returned when a file does not open with a YAML
// frontmatter block.
var ErrNoFrontmatter = errors.New("memory: file has no frontmatter block")

// Memory is one atomic remembered fact.
type Memory struct {
	ID            string
	Created       time.Time
	Author        string
	Scope         Scope
	Tags          []string
	SourceSession string
	Confidence    float64
	Slug          string
	Body          string
}

// frontmatter mirrors Memory minus the Body, with explicit YAML keys so the
// on-disk format is stable regardless of Go field renames.
type frontmatter struct {
	ID            string    `yaml:"id"`
	Created       time.Time `yaml:"created"`
	Author        string    `yaml:"author,omitempty"`
	Scope         Scope     `yaml:"scope,omitempty"`
	Tags          []string  `yaml:"tags,omitempty"`
	SourceSession string    `yaml:"source_session,omitempty"`
	Confidence    float64   `yaml:"confidence,omitempty"`
	Slug          string    `yaml:"slug,omitempty"`
}

// Marshal renders a Memory as frontmatter plus body.
func Marshal(m Memory) ([]byte, error) {
	fm := frontmatter{
		ID:            m.ID,
		Created:       m.Created.UTC(),
		Author:        m.Author,
		Scope:         m.Scope,
		Tags:          m.Tags,
		SourceSession: m.SourceSession,
		Confidence:    m.Confidence,
		Slug:          m.Slug,
	}
	y, err := yaml.Marshal(fm)
	if err != nil {
		return nil, fmt.Errorf("memory: marshal frontmatter: %w", err)
	}

	var buf bytes.Buffer
	buf.Write(delimiter)
	buf.Write(y)
	buf.Write(delimiter)
	buf.WriteString(m.Body)
	if len(m.Body) > 0 && m.Body[len(m.Body)-1] != '\n' {
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// Unmarshal parses frontmatter plus body back into a Memory. A memory with no
// explicit scope defaults to team, matching the spec's rule that ambiguous
// memories are team memories.
func Unmarshal(data []byte) (Memory, error) {
	if !bytes.HasPrefix(data, delimiter) {
		return Memory{}, ErrNoFrontmatter
	}
	rest := data[len(delimiter):]
	end := bytes.Index(rest, delimiter)
	if end < 0 {
		return Memory{}, ErrNoFrontmatter
	}

	var fm frontmatter
	if err := yaml.Unmarshal(rest[:end], &fm); err != nil {
		return Memory{}, fmt.Errorf("memory: parse frontmatter: %w", err)
	}
	if fm.Scope == "" {
		fm.Scope = ScopeTeam
	}

	body := string(bytes.TrimPrefix(rest[end+len(delimiter):], []byte("\n")))
	return Memory{
		ID:            fm.ID,
		Created:       fm.Created,
		Author:        fm.Author,
		Scope:         fm.Scope,
		Tags:          fm.Tags,
		SourceSession: fm.SourceSession,
		Confidence:    fm.Confidence,
		Slug:          fm.Slug,
		Body:          body,
	}, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./memory/ -v`
Expected: PASS — all three tests.

- [ ] **Step 6: Commit**

```bash
git add mcp/memory/memory.go mcp/memory/memory_test.go mcp/go.mod mcp/go.sum
git commit -m "feat(memory): add Memory type with frontmatter round-trip"
```

---

### Task 2: Content-addressed ID and repository path

**Files:**
- Modify: `mcp/memory/memory.go`
- Create: `mcp/memory/path.go`
- Test: `mcp/memory/path_test.go`

**Interfaces:**
- Consumes: `Memory`, `Scope`, `ScopeTeam`, `ScopePersonal` from Task 1.
- Produces:
  - `func ComputeID(m Memory) string` — 16 lowercase hex chars, deterministic, body+author+created only.
  - `func Slugify(s string) string` — lowercase, hyphenated, max 48 chars, ASCII only.
  - `func RepoPath(m Memory) (string, error)` — forward-slash repo-relative path.

Two memories with identical body, author and timestamp collapse to one path. That is intended deduplication, not a bug.

- [ ] **Step 1: Write the failing test**

Create `mcp/memory/path_test.go`:

```go
package memory

import (
	"strings"
	"testing"
	"time"
)

func fixture() Memory {
	return Memory{
		Created: time.Date(2026, 8, 22, 14, 30, 0, 0, time.UTC),
		Author:  "andrew",
		Scope:   ScopeTeam,
		Slug:    "chose-go-git",
		Body:    "We picked go-git.",
	}
}

func TestComputeIDIsDeterministic(t *testing.T) {
	a, b := fixture(), fixture()
	if ComputeID(a) != ComputeID(b) {
		t.Fatal("ComputeID is not deterministic for identical input")
	}
	if got := len(ComputeID(a)); got != 16 {
		t.Errorf("ID length = %d, want 16", got)
	}
}

func TestComputeIDIgnoresScopeAndTags(t *testing.T) {
	a := fixture()
	b := fixture()
	b.Scope = ScopePersonal
	b.Tags = []string{"anything"}
	if ComputeID(a) != ComputeID(b) {
		t.Error("ID must not change when scope or tags are corrected during review")
	}
}

func TestComputeIDChangesWithBody(t *testing.T) {
	a := fixture()
	b := fixture()
	b.Body = "Different content entirely."
	if ComputeID(a) == ComputeID(b) {
		t.Error("different bodies must produce different IDs")
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Chose go-git over the Trees API", "chose-go-git-over-the-trees-api"},
		{"  spaces   everywhere  ", "spaces-everywhere"},
		{"Punctuation!! removed??", "punctuation-removed"},
		{"", "untitled"},
		{"héllo wörld", "h-llo-w-rld"},
		{strings.Repeat("a", 80), strings.Repeat("a", 48)},
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRepoPathTeam(t *testing.T) {
	m := fixture()
	m.ID = ComputeID(m)
	got, err := RepoPath(m)
	if err != nil {
		t.Fatalf("RepoPath: %v", err)
	}
	want := "memories/team/2026/08/" + m.ID + "-chose-go-git.md"
	if got != want {
		t.Errorf("RepoPath = %q, want %q", got, want)
	}
}

func TestRepoPathPersonal(t *testing.T) {
	m := fixture()
	m.Scope = ScopePersonal
	m.ID = ComputeID(m)
	got, err := RepoPath(m)
	if err != nil {
		t.Fatalf("RepoPath: %v", err)
	}
	want := "memories/people/andrew/2026/08/" + m.ID + "-chose-go-git.md"
	if got != want {
		t.Errorf("RepoPath = %q, want %q", got, want)
	}
}

func TestRepoPathPersonalRequiresAuthor(t *testing.T) {
	m := fixture()
	m.Scope = ScopePersonal
	m.Author = ""
	m.ID = ComputeID(m)
	if _, err := RepoPath(m); err == nil {
		t.Fatal("expected error for personal memory with no author")
	}
}

func TestRepoPathRejectsTraversalInAuthor(t *testing.T) {
	m := fixture()
	m.Scope = ScopePersonal
	m.Author = "../../etc"
	m.ID = ComputeID(m)
	if _, err := RepoPath(m); err == nil {
		t.Fatal("expected error for author containing path traversal")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./memory/ -run 'TestCompute|TestSlug|TestRepoPath' -v`
Expected: FAIL — `undefined: ComputeID`.

- [ ] **Step 3: Write the implementation**

Create `mcp/memory/path.go`:

```go
package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	idLen      = 16
	maxSlugLen = 48
)

// ComputeID hashes only the identity-bearing fields: body, author and creation
// time. Scope, tags and slug are review-time corrections and must not move a
// memory to a different path when they change.
func ComputeID(m Memory) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s",
		m.Body, m.Author, m.Created.UTC().Format("2006-01-02T15:04:05Z"))
	return hex.EncodeToString(h.Sum(nil))[:idLen]
}

// Slugify produces a filesystem- and URL-safe fragment. Non-ASCII bytes become
// hyphens rather than being transliterated, so the result is stable across
// platforms without a Unicode table.
func Slugify(s string) string {
	var b strings.Builder
	lastHyphen := true // leading hyphens are suppressed
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > maxSlugLen {
		out = strings.Trim(out[:maxSlugLen], "-")
	}
	if out == "" {
		return "untitled"
	}
	return out
}

// RepoPath returns the repo-relative, forward-slash path for a memory.
// Content addressing means two writers on different machines never target the
// same path for different content, so concurrent additions merge cleanly.
func RepoPath(m Memory) (string, error) {
	if m.ID == "" {
		return "", fmt.Errorf("memory: cannot build path, ID is empty")
	}
	slug := Slugify(m.Slug)
	dateDir := m.Created.UTC().Format("2006/01")
	name := m.ID + "-" + slug + ".md"

	switch m.Scope {
	case ScopePersonal:
		// Reject separators before slugifying. Slugify alone would turn
		// "../../etc" into the harmless-looking "etc", silently filing the
		// memory under the wrong author instead of refusing.
		if strings.ContainsAny(m.Author, `/\`) || strings.Contains(m.Author, "..") {
			return "", fmt.Errorf("memory: author %q contains path separators", m.Author)
		}
		author := Slugify(m.Author)
		if m.Author == "" || author == "untitled" {
			return "", fmt.Errorf("memory: personal memory %s has no usable author", m.ID)
		}
		return "memories/people/" + author + "/" + dateDir + "/" + name, nil
	case ScopeTeam, "":
		return "memories/team/" + dateDir + "/" + name, nil
	default:
		return "", fmt.Errorf("memory: unknown scope %q", m.Scope)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./memory/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add mcp/memory/path.go mcp/memory/path_test.go
git commit -m "feat(memory): add content-addressed IDs and repo paths"
```

---

### Task 3: Home directory layout with the staging boundary

**Files:**
- Create: `mcp/paths/paths.go`
- Test: `mcp/paths/paths_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Layout struct { Home, Clone, Staging, Pending, Bin, Index, TokenFile string }`
  - `func Resolve() (Layout, error)` — honours `$TEAM_MEMORY_HOME`, else per-OS default.
  - `func New(home string) (Layout, error)` — explicit root, for tests.
  - `func (l Layout) EnsureDirs() error`
  - `func (l Layout) IsInsideClone(abs string) (bool, error)`

`Staging` is a **sibling** of `Clone`, never a child. Task 5 depends on this.

- [ ] **Step 1: Write the failing test**

Create `mcp/paths/paths_test.go`:

```go
package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStagingIsSiblingOfClone(t *testing.T) {
	l, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if strings.HasPrefix(l.Staging, l.Clone+string(os.PathSeparator)) {
		t.Fatalf("staging %q is inside clone %q; unreviewed content would be committable",
			l.Staging, l.Clone)
	}
	if filepath.Dir(l.Staging) != filepath.Dir(l.Clone) {
		t.Errorf("staging and clone must share a parent: %q vs %q", l.Staging, l.Clone)
	}
}

func TestEnsureDirsCreatesEverything(t *testing.T) {
	l, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := l.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	for _, d := range []string{l.Clone, l.Staging, l.Pending, l.Bin} {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("stat %q: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q is not a directory", d)
		}
	}
}

func TestEnsureDirsIsIdempotent(t *testing.T) {
	l, _ := New(t.TempDir())
	if err := l.EnsureDirs(); err != nil {
		t.Fatalf("first EnsureDirs: %v", err)
	}
	if err := l.EnsureDirs(); err != nil {
		t.Fatalf("second EnsureDirs: %v", err)
	}
}

func TestIsInsideClone(t *testing.T) {
	l, _ := New(t.TempDir())
	if err := l.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	inside := filepath.Join(l.Clone, "memories", "team", "x.md")
	ok, err := l.IsInsideClone(inside)
	if err != nil {
		t.Fatalf("IsInsideClone(inside): %v", err)
	}
	if !ok {
		t.Errorf("%q should be inside the clone", inside)
	}

	outside := filepath.Join(l.Pending, "x.md")
	ok, err = l.IsInsideClone(outside)
	if err != nil {
		t.Fatalf("IsInsideClone(outside): %v", err)
	}
	if ok {
		t.Errorf("%q must NOT be inside the clone", outside)
	}
}

func TestIsInsideCloneRejectsTraversal(t *testing.T) {
	l, _ := New(t.TempDir())
	_ = l.EnsureDirs()
	escaped := filepath.Join(l.Clone, "..", "staging", "pending", "leak.md")
	ok, err := l.IsInsideClone(escaped)
	if err != nil {
		t.Fatalf("IsInsideClone: %v", err)
	}
	if ok {
		t.Errorf("traversal path %q must not be reported as inside the clone", escaped)
	}
}

func TestResolveHonoursEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEAM_MEMORY_HOME", dir)
	l, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if l.Home != dir {
		t.Errorf("Home = %q, want %q", l.Home, dir)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./paths/ -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write the implementation**

Create `mcp/paths/paths.go`:

```go
// Package paths owns the on-disk layout of team-memory's state directory.
//
// The single most important property here: staging/ is a SIBLING of clone/,
// never a child. Unreviewed content is therefore unreachable by any git
// worktree add pattern, including a buggy glob. The privacy guarantee is a
// property of the layout, not a promise that the commit code is correct.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Layout is a resolved set of absolute paths.
type Layout struct {
	Home      string // $TEAM_MEMORY_HOME
	Clone     string // Home/clone      — the git worktree. Shareable.
	Staging   string // Home/staging    — sibling of Clone. Never committed.
	Pending   string // Home/staging/pending
	Bin       string // Home/staging/bin
	Index     string // Home/index.sqlite — derived, rebuildable
	TokenFile string // Home/token        — mode 0600
}

// New builds a Layout rooted at an explicit absolute home directory.
func New(home string) (Layout, error) {
	abs, err := filepath.Abs(home)
	if err != nil {
		return Layout{}, fmt.Errorf("paths: resolve %q: %w", home, err)
	}
	staging := filepath.Join(abs, "staging")
	return Layout{
		Home:      abs,
		Clone:     filepath.Join(abs, "clone"),
		Staging:   staging,
		Pending:   filepath.Join(staging, "pending"),
		Bin:       filepath.Join(staging, "bin"),
		Index:     filepath.Join(abs, "index.sqlite"),
		TokenFile: filepath.Join(abs, "token"),
	}, nil
}

// Resolve picks the state directory: $TEAM_MEMORY_HOME if set, otherwise the
// per-OS default.
func Resolve() (Layout, error) {
	if h := os.Getenv("TEAM_MEMORY_HOME"); h != "" {
		return New(h)
	}
	base, err := defaultBase()
	if err != nil {
		return Layout{}, err
	}
	return New(filepath.Join(base, "team-memory"))
}

func defaultBase() (string, error) {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return d, nil
		}
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("paths: locate home directory: %w", err)
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support"), nil
	}
	return filepath.Join(home, ".local", "state"), nil
}

// EnsureDirs creates every directory in the layout. Idempotent.
func EnsureDirsOf(l Layout) error { return l.EnsureDirs() }

// EnsureDirs creates every directory in the layout. Idempotent.
func (l Layout) EnsureDirs() error {
	for _, d := range []string{l.Home, l.Clone, l.Staging, l.Pending, l.Bin} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("paths: create %q: %w", d, err)
		}
	}
	return nil
}

// IsInsideClone reports whether abs resolves to a location within the clone
// worktree. Symlinks are resolved where the path exists, so a symlink pointing
// out of the worktree cannot masquerade as inside it.
func (l Layout) IsInsideClone(abs string) (bool, error) {
	clone, err := resolveExisting(l.Clone)
	if err != nil {
		return false, err
	}
	target, err := resolveExisting(abs)
	if err != nil {
		return false, err
	}
	if target == clone {
		return true, nil
	}
	return strings.HasPrefix(target, clone+string(os.PathSeparator)), nil
}

// resolveExisting cleans a path and resolves symlinks on the longest existing
// prefix, so paths that do not exist yet still normalise correctly.
func resolveExisting(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("paths: abs %q: %w", p, err)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(real), nil
	}
	dir, file := filepath.Split(abs)
	realDir, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return filepath.Clean(abs), nil
	}
	return filepath.Join(realDir, file), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./paths/ -v`
Expected: PASS — all six tests.

- [ ] **Step 5: Commit**

```bash
git add mcp/paths/
git commit -m "feat(paths): add state layout with staging as a clone sibling"
```

---

### Task 4: Open or clone the repository, fast-forward-only sync

**Files:**
- Create: `mcp/store/store.go`
- Test: `mcp/store/store_test.go`

**Interfaces:**
- Consumes: `paths.Layout` from Task 3.
- Produces:
  - `type Store struct { … }` with unexported fields
  - `func Open(l paths.Layout) (*Store, error)` — opens an existing clone, or initialises an empty one.
  - `func (s *Store) Worktree() (*git.Worktree, error)`
  - `func (s *Store) Sync(ctx context.Context) error` — fetch plus fast-forward-only merge.
  - `var ErrNotFastForward = errors.New("store: remote has diverged; refusing non-fast-forward merge")`

- [ ] **Step 1: Add the go-git dependency**

```bash
cd mcp && GOFLAGS=-mod=mod go get github.com/go-git/go-git/v5@v5.19.2
```

Verify the resolved version is at least v5.19.2 — earlier versions carry symlink
and path-traversal advisories:

```bash
cd mcp && go list -m github.com/go-git/go-git/v5
```

- [ ] **Step 2: Write the failing test**

Create `mcp/store/store_test.go`:

```go
package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/paths"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// newLayout returns an initialised layout rooted in a temp dir.
func newLayout(t *testing.T) paths.Layout {
	t.Helper()
	l, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatalf("paths.New: %v", err)
	}
	if err := l.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	return l
}

func TestOpenInitialisesEmptyClone(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.Worktree(); err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(l.Clone, ".git")); err != nil {
		t.Errorf("expected a .git directory in the clone: %v", err)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	l := newLayout(t)
	if _, err := Open(l); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := Open(l); err != nil {
		t.Fatalf("second Open: %v", err)
	}
}

func TestSyncFastForwards(t *testing.T) {
	// Build an upstream repo with one commit.
	upstream := t.TempDir()
	up, err := git.PlainInit(upstream, false)
	if err != nil {
		t.Fatalf("PlainInit upstream: %v", err)
	}
	upWT, _ := up.Worktree()
	writeFile(t, filepath.Join(upstream, "README.md"), "one\n")
	if _, err := upWT.Add("README.md"); err != nil {
		t.Fatalf("upstream Add: %v", err)
	}
	if _, err := upWT.Commit("first", commitOpts()); err != nil {
		t.Fatalf("upstream Commit: %v", err)
	}

	// Clone it, then advance upstream by one commit.
	l := newLayout(t)
	if err := os.RemoveAll(l.Clone); err != nil {
		t.Fatalf("RemoveAll clone: %v", err)
	}
	if _, err := git.PlainClone(l.Clone, false, &git.CloneOptions{URL: upstream}); err != nil {
		t.Fatalf("PlainClone: %v", err)
	}
	writeFile(t, filepath.Join(upstream, "SECOND.md"), "two\n")
	if _, err := upWT.Add("SECOND.md"); err != nil {
		t.Fatalf("upstream Add 2: %v", err)
	}
	if _, err := upWT.Commit("second", commitOpts()); err != nil {
		t.Fatalf("upstream Commit 2: %v", err)
	}

	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(l.Clone, "SECOND.md")); err != nil {
		t.Errorf("expected SECOND.md after fast-forward: %v", err)
	}
}

func commitOpts() *git.CommitOptions {
	return &git.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@example.com"}}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./store/ -v`
Expected: FAIL — `undefined: Open`.

- [ ] **Step 4: Write the implementation**

Create `mcp/store/store.go`:

```go
// Package store wraps the local git clone that holds committed memories.
//
// Reads are plain filesystem reads: instant, offline, and never rate limited.
// Sync is fetch plus fast-forward-only merge. This package never force-pushes,
// never rewrites history, and never runs gc.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/AndrewSkea/team-memory/mcp/paths"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
)

// ErrNotFastForward means the remote has commits the local clone cannot reach
// by fast-forward. Resolving that is a user decision, not something to
// automate.
var ErrNotFastForward = errors.New("store: remote has diverged; refusing non-fast-forward merge")

// Store is a handle on the local clone.
type Store struct {
	layout paths.Layout
	repo   *git.Repository
}

// Open opens the clone, initialising an empty repository if none exists.
func Open(l paths.Layout) (*Store, error) {
	if err := l.EnsureDirs(); err != nil {
		return nil, err
	}
	repo, err := git.PlainOpen(l.Clone)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		repo, err = git.PlainInit(l.Clone, false)
	}
	if err != nil {
		return nil, fmt.Errorf("store: open clone at %q: %w", l.Clone, err)
	}
	return &Store{layout: l, repo: repo}, nil
}

// Layout exposes the paths this store was opened with.
func (s *Store) Layout() paths.Layout { return s.layout }

// Worktree returns the clone's worktree.
func (s *Store) Worktree() (*git.Worktree, error) {
	wt, err := s.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("store: worktree: %w", err)
	}
	return wt, nil
}

// Sync fetches from origin and fast-forwards the checked-out branch. A
// repository with no remote is a valid local-only state and syncs to a no-op.
func (s *Store) Sync(ctx context.Context) error {
	if _, err := s.repo.Remote(git.DefaultRemoteName); err != nil {
		if errors.Is(err, git.ErrRemoteNotFound) {
			return nil
		}
		return fmt.Errorf("store: lookup remote: %w", err)
	}

	err := s.repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: git.DefaultRemoteName,
		Force:      false,
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("store: fetch: %w", err)
	}

	head, err := s.repo.Head()
	if err != nil {
		// An empty repository has no HEAD yet. Nothing to fast-forward onto.
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil
		}
		return fmt.Errorf("store: head: %w", err)
	}

	remoteRef := plumbing.NewRemoteReferenceName(git.DefaultRemoteName, head.Name().Short())
	remote, err := s.repo.Reference(remoteRef, true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil // branch does not exist upstream yet
		}
		return fmt.Errorf("store: resolve %s: %w", remoteRef, err)
	}
	if remote.Hash() == head.Hash() {
		return nil
	}

	ff, err := s.isAncestor(head.Hash(), remote.Hash())
	if err != nil {
		return err
	}
	if !ff {
		return ErrNotFastForward
	}

	wt, err := s.Worktree()
	if err != nil {
		return err
	}
	if err := wt.Checkout(&git.CheckoutOptions{Hash: remote.Hash()}); err != nil {
		return fmt.Errorf("store: fast-forward checkout: %w", err)
	}
	ref := plumbing.NewHashReference(head.Name(), remote.Hash())
	if err := s.repo.Storer.SetReference(ref); err != nil {
		return fmt.Errorf("store: update %s: %w", head.Name(), err)
	}
	return nil
}

// isAncestor reports whether ancestor is reachable from descendant.
func (s *Store) isAncestor(ancestor, descendant plumbing.Hash) (bool, error) {
	a, err := s.repo.CommitObject(ancestor)
	if err != nil {
		return false, fmt.Errorf("store: load commit %s: %w", ancestor, err)
	}
	d, err := s.repo.CommitObject(descendant)
	if err != nil {
		return false, fmt.Errorf("store: load commit %s: %w", descendant, err)
	}
	ok, err := a.IsAncestor(d)
	if err != nil {
		return false, fmt.Errorf("store: ancestry check: %w", err)
	}
	return ok, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./store/ -v`
Expected: PASS — three tests.

- [ ] **Step 6: Commit**

```bash
git add mcp/store/store.go mcp/store/store_test.go mcp/go.mod mcp/go.sum
git commit -m "feat(store): open local clone with fast-forward-only sync"
```

---

### Task 5: The commit guard — the privacy invariant

**Files:**
- Create: `mcp/store/commit.go`
- Test: `mcp/store/commit_test.go`

**Interfaces:**
- Consumes: `Store` from Task 4, `paths.Layout` from Task 3, `memory.Memory` and `memory.RepoPath` from Tasks 1–2.
- Produces:
  - `func (s *Store) Put(m memory.Memory, msg string) (plumbing.Hash, error)`
  - `func (s *Store) Remove(repoPath, msg string) (plumbing.Hash, error)`
  - `var ErrOutsideClone = errors.New("store: refusing to commit a path outside the clone")`

This is the load-bearing task of the plan. `TestPutRefusesStagingPath` and
`TestPutRefusesTraversalEscape` are **permanent regression tests** — they must
never be deleted, skipped, or weakened.

- [ ] **Step 1: Write the failing test**

Create `mcp/store/commit_test.go`:

```go
package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/memory"
)

func newMemory(body string) memory.Memory {
	m := memory.Memory{
		Created: time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC),
		Author:  "andrew",
		Scope:   memory.ScopeTeam,
		Slug:    "a-decision",
		Body:    body,
	}
	m.ID = memory.ComputeID(m)
	return m
}

func TestPutWritesAndCommits(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	m := newMemory("We chose go-git.")
	if _, err := s.Put(m, "add a decision"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	rel, _ := memory.RepoPath(m)
	onDisk := filepath.Join(l.Clone, filepath.FromSlash(rel))
	data, err := os.ReadFile(onDisk)
	if err != nil {
		t.Fatalf("read committed file: %v", err)
	}
	got, err := memory.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Body != m.Body {
		t.Errorf("Body = %q, want %q", got.Body, m.Body)
	}

	wt, _ := s.Worktree()
	status, err := wt.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.IsClean() {
		t.Errorf("worktree should be clean after Put, got %v", status)
	}
}

// PERMANENT REGRESSION TEST. Do not delete, skip, or weaken.
// The privacy guarantee of the entire product is that unreviewed content in
// staging/ can never be committed to the shared repository.
func TestPutRefusesStagingPath(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	leak := filepath.Join(l.Pending, "unreviewed.md")
	if err := os.WriteFile(leak, []byte("secret thinking out loud"), 0o600); err != nil {
		t.Fatalf("write pending file: %v", err)
	}

	if _, err := s.commitPath(leak, "should never happen"); !errors.Is(err, ErrOutsideClone) {
		t.Fatalf("committing a staging path returned %v, want ErrOutsideClone", err)
	}
}

// PERMANENT REGRESSION TEST. Do not delete, skip, or weaken.
func TestPutRefusesTraversalEscape(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	escaped := filepath.Join(l.Clone, "..", "staging", "pending", "leak.md")
	if err := os.WriteFile(filepath.Clean(escaped), []byte("leak"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := s.commitPath(escaped, "should never happen"); !errors.Is(err, ErrOutsideClone) {
		t.Fatalf("traversal path returned %v, want ErrOutsideClone", err)
	}
}

// Two machines adding different memories must merge with no conflict, because
// content addressing guarantees they never target the same path.
func TestConcurrentAdditionsUseDistinctPaths(t *testing.T) {
	a := newMemory("Machine A learned something.")
	b := newMemory("Machine B learned something else.")

	pa, err := memory.RepoPath(a)
	if err != nil {
		t.Fatalf("RepoPath(a): %v", err)
	}
	pb, err := memory.RepoPath(b)
	if err != nil {
		t.Fatalf("RepoPath(b): %v", err)
	}
	if pa == pb {
		t.Fatalf("distinct memories collided on path %q", pa)
	}
	if !strings.HasPrefix(pa, "memories/team/2026/08/") {
		t.Errorf("unexpected path shape: %q", pa)
	}
}

func TestRemoveDeletesAndCommits(t *testing.T) {
	l := newLayout(t)
	s, _ := Open(l)

	m := newMemory("Temporary.")
	if _, err := s.Put(m, "add"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rel, _ := memory.RepoPath(m)
	if _, err := s.Remove(rel, "remove from memory"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(l.Clone, filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Errorf("file should be gone, stat err = %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./store/ -run TestPut -v`
Expected: FAIL — `s.Put undefined`.

- [ ] **Step 3: Write the implementation**

Create `mcp/store/commit.go`:

```go
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// ErrOutsideClone is returned when a commit is attempted on a path that does
// not resolve inside the clone worktree. Every write funnels through the check
// that produces this error.
var ErrOutsideClone = errors.New("store: refusing to commit a path outside the clone")

// Put writes a memory to its content-addressed path and commits it.
func (s *Store) Put(m memory.Memory, msg string) (plumbing.Hash, error) {
	rel, err := memory.RepoPath(m)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	data, err := memory.Marshal(m)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	abs := filepath.Join(s.layout.Clone, filepath.FromSlash(rel))
	if err := s.assertInsideClone(abs); err != nil {
		return plumbing.ZeroHash, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store: create directory for %q: %w", rel, err)
	}
	if err := os.WriteFile(abs, data, 0o600); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store: write %q: %w", rel, err)
	}
	return s.commitPath(abs, msg)
}

// Remove deletes a committed memory file and commits the deletion. The blob
// remains in git history — see the spec's §5.6 on describing deletion honestly.
func (s *Store) Remove(repoPath, msg string) (plumbing.Hash, error) {
	abs := filepath.Join(s.layout.Clone, filepath.FromSlash(repoPath))
	if err := s.assertInsideClone(abs); err != nil {
		return plumbing.ZeroHash, err
	}
	if err := os.Remove(abs); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store: remove %q: %w", repoPath, err)
	}
	return s.commitPath(abs, msg)
}

// assertInsideClone is the guard. Every path that reaches git passes through
// here first.
func (s *Store) assertInsideClone(abs string) error {
	inside, err := s.layout.IsInsideClone(abs)
	if err != nil {
		return fmt.Errorf("store: check path %q: %w", abs, err)
	}
	if !inside {
		return fmt.Errorf("%w: %s", ErrOutsideClone, abs)
	}
	return nil
}

// commitPath stages one absolute path and commits it. It re-runs the guard so
// that no future caller can bypass the check by calling this directly.
func (s *Store) commitPath(abs, msg string) (plumbing.Hash, error) {
	if err := s.assertInsideClone(abs); err != nil {
		return plumbing.ZeroHash, err
	}
	wt, err := s.Worktree()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	rel, err := filepath.Rel(s.layout.Clone, abs)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store: relativise %q: %w", abs, err)
	}
	slashed := filepath.ToSlash(rel)

	if _, err := wt.Add(slashed); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store: stage %q: %w", slashed, err)
	}
	hash, err := wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "team-memory",
			Email: "team-memory@localhost",
			When:  time.Now(),
		},
	})
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store: commit %q: %w", slashed, err)
	}
	return hash, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./store/ -v`
Expected: PASS — including both permanent regression tests.

- [ ] **Step 5: Guard against future weakening**

Confirm the guard cannot be bypassed by an author who adds a new write path.
Search for any direct call to `wt.Add` outside `commitPath`:

```bash
cd mcp && grep -rn "\.Add(" store/ | grep -v commitPath
```

Expected: no results other than the definition inside `commitPath`.

- [ ] **Step 6: Commit**

```bash
git add mcp/store/commit.go mcp/store/commit_test.go
git commit -m "feat(store): add commit guard preventing staging leakage"
```

---

### Task 6: Load all memories and regenerate INDEX.md

**Files:**
- Create: `mcp/store/scan.go`
- Test: `mcp/store/scan_test.go`

**Interfaces:**
- Consumes: `Store` from Task 4, `memory.Memory` from Task 1.
- Produces:
  - `func (s *Store) All() ([]memory.Memory, error)` — sorted by `Created` descending, then `ID` ascending, so output is deterministic.
  - `func (s *Store) RegenerateIndex() (plumbing.Hash, error)`
  - `func RenderIndex(ms []memory.Memory) string`

`INDEX.md` is a derived artifact. Nothing reads it as authoritative state.

- [ ] **Step 1: Write the failing test**

Create `mcp/store/scan_test.go`:

```go
package store

import (
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./store/ -run 'TestAll|TestRender|TestRegenerate' -v`
Expected: FAIL — `s.All undefined`.

- [ ] **Step 3: Write the implementation**

Create `mcp/store/scan.go`:

```go
package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/go-git/go-git/v5/plumbing"
)

const memoriesDir = "memories"

// All loads every parsable memory from the clone, newest first. A file that
// fails to parse is skipped rather than failing the whole scan — one corrupt
// file must not make the entire memory unreadable.
func (s *Store) All() ([]memory.Memory, error) {
	root := filepath.Join(s.layout.Clone, memoriesDir)
	var out []memory.Memory

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil // no memories yet
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		m, parseErr := memory.Unmarshal(data)
		if parseErr != nil {
			return nil
		}
		out = append(out, m)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: scan memories: %w", err)
	}

	sort.Slice(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.After(out[j].Created)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// RenderIndex produces INDEX.md from the memories themselves. It is a derived
// artifact: regenerating it from the same input always yields the same bytes,
// so it never becomes a merge-conflict source.
func RenderIndex(ms []memory.Memory) string {
	var b strings.Builder
	b.WriteString("# INDEX for team-memory\n\n")
	b.WriteString("Generated file. Do not edit by hand.\n\n")
	b.WriteString("| id | created | scope | author | tags | title |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, m := range ms {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			m.ID,
			m.Created.UTC().Format("2006-01-02"),
			m.Scope,
			m.Author,
			strings.Join(m.Tags, " "),
			m.Slug,
		)
	}
	return b.String()
}

// RegenerateIndex rewrites INDEX.md from the memories on disk and commits it.
func (s *Store) RegenerateIndex() (plumbing.Hash, error) {
	ms, err := s.All()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	abs := filepath.Join(s.layout.Clone, "INDEX.md")
	if err := s.assertInsideClone(abs); err != nil {
		return plumbing.ZeroHash, err
	}
	if err := os.WriteFile(abs, []byte(RenderIndex(ms)), 0o600); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store: write INDEX.md: %w", err)
	}
	return s.commitPath(abs, "team-memory: regenerate INDEX.md")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./store/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add mcp/store/scan.go mcp/store/scan_test.go
git commit -m "feat(store): scan memories and regenerate INDEX.md deterministically"
```

---

### Task 7: Rebuildable full-text search index

**Files:**
- Create: `mcp/search/search.go`
- Test: `mcp/search/search_test.go`

**Interfaces:**
- Consumes: `memory.Memory` from Task 1, `paths.Layout` from Task 3.
- Produces:
  - `type Index struct { … }`
  - `func Open(dbPath string) (*Index, error)`
  - `func (ix *Index) Rebuild(ms []memory.Memory) error`
  - `func (ix *Index) Search(q string, limit int) ([]Hit, error)`
  - `type Hit struct { ID, Slug, Snippet string; Scope memory.Scope }`
  - `func (ix *Index) Close() error`

The index is derived and disposable. Deleting the file must be recoverable by
calling `Rebuild` with the output of `store.All()`.

- [ ] **Step 1: Add the sqlite dependency**

```bash
cd mcp && GOFLAGS=-mod=mod go get modernc.org/sqlite@v1.57.0
```

FTS5 is compiled in with **no build tag** — already verified against v1.57.0
under `CGO_ENABLED=0`.

- [ ] **Step 2: Write the failing test**

Create `mcp/search/search_test.go`:

```go
package search

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/memory"
)

func sample() []memory.Memory {
	return []memory.Memory{
		{
			ID: "1111111111111111", Slug: "chose-go-git", Scope: memory.ScopeTeam,
			Created: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			Tags:    []string{"storage"},
			Body:    "We picked go-git because reads are local and offline.",
		},
		{
			ID: "2222222222222222", Slug: "editor-setup", Scope: memory.ScopePersonal,
			Created: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
			Author:  "andrew",
			Body:    "I prefer two-space indentation in this repository.",
		},
	}
}

func openTemp(t *testing.T) (*Index, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "index.sqlite")
	ix, err := Open(p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	return ix, p
}

func TestSearchFindsByBody(t *testing.T) {
	ix, _ := openTemp(t)
	if err := ix.Rebuild(sample()); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	hits, err := ix.Search("offline", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "1111111111111111" {
		t.Fatalf("Search(offline) = %+v, want the go-git memory", hits)
	}
}

func TestSearchFindsBySlug(t *testing.T) {
	ix, _ := openTemp(t)
	_ = ix.Rebuild(sample())
	hits, err := ix.Search("editor", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "2222222222222222" {
		t.Fatalf("Search(editor) = %+v, want the editor-setup memory", hits)
	}
}

func TestRebuildIsIdempotent(t *testing.T) {
	ix, _ := openTemp(t)
	for i := 0; i < 3; i++ {
		if err := ix.Rebuild(sample()); err != nil {
			t.Fatalf("Rebuild %d: %v", i, err)
		}
	}
	hits, err := ix.Search("go-git", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("got %d hits after 3 rebuilds, want 1 — rebuild is not replacing rows", len(hits))
	}
}

func TestIndexIsRecoverableAfterDeletion(t *testing.T) {
	ix, path := openTemp(t)
	_ = ix.Rebuild(sample())
	if err := ix.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after deletion: %v", err)
	}
	defer reopened.Close()
	if err := reopened.Rebuild(sample()); err != nil {
		t.Fatalf("Rebuild after deletion: %v", err)
	}
	hits, err := reopened.Search("offline", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("index did not recover: got %d hits, want 1", len(hits))
	}
}

func TestSearchEmptyQueryReturnsNothing(t *testing.T) {
	ix, _ := openTemp(t)
	_ = ix.Rebuild(sample())
	hits, err := ix.Search("   ", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("empty query returned %d hits, want 0", len(hits))
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./search/ -v`
Expected: FAIL — `undefined: Open`.

- [ ] **Step 4: Write the implementation**

Create `mcp/search/search.go`:

```go
// Package search provides full-text search over memories.
//
// The index is a fully derived, disposable artifact. Delete the database and
// call Rebuild to reconstruct it from the Markdown files. Because it is never
// a source of truth, index corruption is never data loss.
package search

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/AndrewSkea/team-memory/mcp/memory"
	_ "modernc.org/sqlite"
)

// Hit is one search result.
type Hit struct {
	ID      string
	Slug    string
	Scope   memory.Scope
	Snippet string
}

// Index wraps the SQLite FTS5 database.
type Index struct {
	db *sql.DB
}

const schema = `
CREATE VIRTUAL TABLE IF NOT EXISTS memories USING fts5(
    id UNINDEXED,
    slug,
    scope UNINDEXED,
    author,
    tags,
    body
);`

// Open opens or creates the index database.
func Open(dbPath string) (*Index, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("search: open %q: %w", dbPath, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("search: create schema: %w", err)
	}
	return &Index{db: db}, nil
}

// Close releases the database handle.
func (ix *Index) Close() error { return ix.db.Close() }

// Rebuild replaces the entire index contents. Idempotent by construction: the
// table is emptied first, so repeated rebuilds never duplicate rows.
func (ix *Index) Rebuild(ms []memory.Memory) error {
	tx, err := ix.db.Begin()
	if err != nil {
		return fmt.Errorf("search: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM memories`); err != nil {
		return fmt.Errorf("search: clear index: %w", err)
	}
	stmt, err := tx.Prepare(
		`INSERT INTO memories (id, slug, scope, author, tags, body) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("search: prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, m := range ms {
		_, err := stmt.Exec(
			m.ID,
			strings.ReplaceAll(m.Slug, "-", " "),
			string(m.Scope),
			m.Author,
			strings.Join(m.Tags, " "),
			m.Body,
		)
		if err != nil {
			return fmt.Errorf("search: index memory %s: %w", m.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("search: commit index: %w", err)
	}
	return nil
}

// Search runs an FTS5 MATCH query. The user's input is passed as a bound
// parameter and wrapped as a quoted phrase, so FTS5 operator characters in the
// query cannot change the query's structure.
func (ix *Index) Search(q string, limit int) ([]Hit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	phrase := `"` + strings.ReplaceAll(q, `"`, `""`) + `"`

	rows, err := ix.db.Query(`
        SELECT id, slug, scope, snippet(memories, 5, '', '', '…', 20)
        FROM memories
        WHERE memories MATCH ?
        ORDER BY rank
        LIMIT ?`, phrase, limit)
	if err != nil {
		return nil, fmt.Errorf("search: query %q: %w", q, err)
	}
	defer rows.Close()

	var out []Hit
	for rows.Next() {
		var h Hit
		var scope string
		if err := rows.Scan(&h.ID, &h.Slug, &scope, &h.Snippet); err != nil {
			return nil, fmt.Errorf("search: scan row: %w", err)
		}
		h.Scope = memory.Scope(scope)
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search: iterate rows: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./search/ -v`
Expected: PASS — five tests.

- [ ] **Step 6: Confirm the build is still cgo-free**

```bash
cd mcp && CGO_ENABLED=0 go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 7: Commit**

```bash
git add mcp/search/ mcp/go.mod mcp/go.sum
git commit -m "feat(search): add rebuildable SQLite FTS5 index"
```

---

### Task 8: Staging area — pending and bin

**Files:**
- Create: `mcp/staging/staging.go`
- Test: `mcp/staging/staging_test.go`

**Interfaces:**
- Consumes: `paths.Layout` from Task 3, `memory.Memory` and `memory.Marshal`/`Unmarshal` from Task 1.
- Produces:
  - `type Area struct { … }`
  - `func New(l paths.Layout) (*Area, error)`
  - `func (a *Area) Add(m memory.Memory) error`
  - `func (a *Area) Pending() ([]memory.Memory, error)` — sorted by `SourceSession`, then `Created`.
  - `func (a *Area) Reject(id, reason string) error` — moves pending → bin.
  - `func (a *Area) Restore(id string) error` — moves bin → pending.
  - `func (a *Area) Take(id string) (memory.Memory, error)` — reads and removes from pending, for approval.
  - `func (a *Area) Binned() ([]BinItem, error)`
  - `type BinItem struct { Memory memory.Memory; Reason string; RejectedAt time.Time }`

- [ ] **Step 1: Write the failing test**

Create `mcp/staging/staging_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./staging/ -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write the implementation**

Create `mcp/staging/staging.go`:

```go
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

// rejection is the sidecar JSON written next to a binned memory.
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

// validID rejects anything that could escape the staging directory.
func validID(id string) error {
	if id == "" {
		return fmt.Errorf("staging: empty id")
	}
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return fmt.Errorf("staging: id %q contains path separators", id)
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

// Pending lists candidates awaiting review, grouped so that memories from the
// same session sort together — the review queue reviews by session.
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./staging/ -v`
Expected: PASS — seven tests.

- [ ] **Step 5: Commit**

```bash
git add mcp/staging/
git commit -m "feat(staging): add pending and bin areas outside the clone"
```

---

### Task 9: Migrate v1 entries to v2 files

**Files:**
- Create: `mcp/migrate/parse.go`
- Create: `mcp/migrate/migrate.go`
- Test: `mcp/migrate/parse_test.go`
- Test: `mcp/migrate/migrate_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–8.
- Produces:
  - `func ParseEntries(source, content string) ([]memory.Memory, []Unparsed)`
  - `type Unparsed struct { Source, Raw, Reason string }`
  - `type Result struct { Converted, Unparsed, Pending int }`
  - `func Run(s *store.Store, a *staging.Area) (Result, error)`

v1 entries are the blocks rendered by `mcp/hook/render.go`: a `### Entry:` header
followed by `**Scope:**`, `**Type:**`, `**Tags:**`, `**Source:**`, `**Summary:**`,
`**Bullets:**` and `**Full:**` lines.

Migration never rewrites history, is re-runnable, and never drops an entry:
anything unparsable is written to `memories/team/unparsed/` and counted.

- [ ] **Step 1: Write the failing parser test**

Create `mcp/migrate/parse_test.go`:

```go
package migrate

import (
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/memory"
)

const v1Doc = `# Some topic

### Entry: 2026-05-12T09:30:00Z — Chose go-git over the Trees API
**Scope:** shared
**Type:** decision
**Tags:** storage, git
**Source:** session-abc
**Summary:** Local clone beats the API for reads.
**Bullets:** - instant reads ; - works offline
**Full:**
We compared the Git Trees API with a local clone.

### Entry: 2026-06-01T11:00:00Z — Andrew prefers two-space indent
**Scope:** personal
**Type:** preference
**Tags:** style
**Source:** session-def
**Summary:** Editor preference.
**Bullets:** - two spaces
**Full:**
Applies to this repository only.
`

func TestParseEntriesExtractsBoth(t *testing.T) {
	got, bad := ParseEntries("TOPIC.md", v1Doc)
	if len(bad) != 0 {
		t.Fatalf("unexpected unparsed entries: %+v", bad)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}

	first := got[0]
	if first.Slug != "chose-go-git-over-the-trees-api" {
		t.Errorf("Slug = %q", first.Slug)
	}
	if first.Scope != memory.ScopeTeam {
		t.Errorf("Scope = %q, want team (v1 'shared' maps to team)", first.Scope)
	}
	if len(first.Tags) != 2 || first.Tags[0] != "storage" {
		t.Errorf("Tags = %v, want [storage git]", first.Tags)
	}
	if first.SourceSession != "session-abc" {
		t.Errorf("SourceSession = %q", first.SourceSession)
	}
	if first.Created.Year() != 2026 || first.Created.Month() != 5 {
		t.Errorf("Created = %v, want May 2026", first.Created)
	}
	if first.ID == "" {
		t.Error("ID must be computed during parse")
	}

	if got[1].Scope != memory.ScopePersonal {
		t.Errorf("second entry Scope = %q, want personal", got[1].Scope)
	}
}

func TestParseEntriesReportsUnparsable(t *testing.T) {
	doc := "### Entry: not-a-timestamp — Broken\n**Scope:** shared\n**Full:**\nbody\n"
	got, bad := ParseEntries("BROKEN.md", doc)
	if len(got) != 0 {
		t.Errorf("got %d parsed, want 0", len(got))
	}
	if len(bad) != 1 {
		t.Fatalf("got %d unparsed, want 1", len(bad))
	}
	if bad[0].Source != "BROKEN.md" || bad[0].Raw == "" || bad[0].Reason == "" {
		t.Errorf("Unparsed missing fields: %+v", bad[0])
	}
}

func TestParseEntriesIgnoresDocumentsWithNoEntries(t *testing.T) {
	got, bad := ParseEntries("README.md", "# Just a heading\n\nSome prose.\n")
	if len(got) != 0 || len(bad) != 0 {
		t.Errorf("got %d parsed and %d unparsed, want 0 and 0", len(got), len(bad))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./migrate/ -v`
Expected: FAIL — `undefined: ParseEntries`.

- [ ] **Step 3: Write the parser**

Create `mcp/migrate/parse.go`:

```go
// Package migrate converts a v1 team-memory repository to the v2 layout.
//
// Migration is idempotent, re-runnable, and never rewrites history. An entry
// that cannot be parsed is preserved verbatim and reported rather than dropped.
package migrate

import (
	"fmt"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/memory"
)

const entryPrefix = "### Entry:"

// Unparsed is a v1 block that could not be converted.
type Unparsed struct {
	Source string
	Raw    string
	Reason string
}

// ParseEntries splits a v1 Markdown document into memories and failures.
func ParseEntries(source, content string) ([]memory.Memory, []Unparsed) {
	var (
		out []memory.Memory
		bad []Unparsed
	)
	for _, block := range splitBlocks(content) {
		m, err := parseBlock(block)
		if err != nil {
			bad = append(bad, Unparsed{Source: source, Raw: block, Reason: err.Error()})
			continue
		}
		out = append(out, m)
	}
	return out, bad
}

// splitBlocks returns each "### Entry:" block, header line included.
func splitBlocks(content string) []string {
	lines := strings.Split(content, "\n")
	var blocks []string
	var cur []string
	for _, line := range lines {
		if strings.HasPrefix(line, entryPrefix) {
			if len(cur) > 0 {
				blocks = append(blocks, strings.Join(cur, "\n"))
			}
			cur = []string{line}
			continue
		}
		if len(cur) > 0 {
			cur = append(cur, line)
		}
	}
	if len(cur) > 0 {
		blocks = append(blocks, strings.Join(cur, "\n"))
	}
	return blocks
}

func parseBlock(block string) (memory.Memory, error) {
	lines := strings.Split(block, "\n")
	header := strings.TrimSpace(strings.TrimPrefix(lines[0], entryPrefix))

	// "2026-05-12T09:30:00Z — Title". The separator is an em dash.
	ts, title, ok := strings.Cut(header, "—")
	if !ok {
		return memory.Memory{}, fmt.Errorf("header has no em-dash separator")
	}
	created, err := time.Parse(time.RFC3339, strings.TrimSpace(ts))
	if err != nil {
		return memory.Memory{}, fmt.Errorf("unparsable timestamp %q", strings.TrimSpace(ts))
	}

	fields := map[string]string{}
	var full []string
	inFull := false
	for _, line := range lines[1:] {
		if inFull {
			full = append(full, line)
			continue
		}
		if strings.TrimSpace(line) == "**Full:**" {
			inFull = true
			continue
		}
		if key, val, ok := cutField(line); ok {
			fields[key] = val
		}
	}

	m := memory.Memory{
		Created:       created,
		Slug:          strings.TrimSpace(title),
		Scope:         mapScope(fields["Scope"]),
		Tags:          splitTags(fields["Tags"]),
		SourceSession: fields["Source"],
		Body:          buildBody(fields["Summary"], fields["Bullets"], full),
	}
	if m.Body == "" {
		return memory.Memory{}, fmt.Errorf("entry has no body")
	}
	m.ID = memory.ComputeID(m)
	return m, nil
}

// cutField parses a "**Key:** value" line.
func cutField(line string) (key, val string, ok bool) {
	if !strings.HasPrefix(line, "**") {
		return "", "", false
	}
	rest := strings.TrimPrefix(line, "**")
	key, val, ok = strings.Cut(rest, ":**")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(val), true
}

// mapScope translates v1 scope words. v1 "shared" is v2 "team". Anything
// unrecognised defaults to team, per the spec's rule on ambiguity.
func mapScope(v1 string) memory.Scope {
	if strings.EqualFold(strings.TrimSpace(v1), "personal") {
		return memory.ScopePersonal
	}
	return memory.ScopeTeam
}

func splitTags(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// buildBody reassembles the v1 summary, bullets and full text into Markdown.
func buildBody(summary, bullets string, full []string) string {
	var b strings.Builder
	if summary != "" {
		b.WriteString(summary)
		b.WriteString("\n")
	}
	for _, item := range strings.Split(bullets, ";") {
		item = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(item), "-"))
		if item != "" {
			b.WriteString("\n- " + item)
		}
	}
	body := strings.TrimSpace(b.String())
	if rest := strings.TrimSpace(strings.Join(full, "\n")); rest != "" {
		if body != "" {
			body += "\n\n"
		}
		body += rest
	}
	return body
}
```

- [ ] **Step 4: Run the parser tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./migrate/ -v`
Expected: PASS — three tests.

- [ ] **Step 5: Commit the parser**

```bash
git add mcp/migrate/parse.go mcp/migrate/parse_test.go
git commit -m "feat(migrate): parse v1 entry blocks into memories"
```

- [ ] **Step 6: Write the failing migration-run test**

Create `mcp/migrate/migrate_test.go`:

```go
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
```

- [ ] **Step 7: Run the test to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./migrate/ -run TestRun -v`
Expected: FAIL — `undefined: Run`.

- [ ] **Step 8: Write the migration runner**

Create `mcp/migrate/migrate.go`:

```go
package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/staging"
	"github.com/AndrewSkea/team-memory/mcp/store"
)

// Result counts what a migration run did.
type Result struct {
	Converted int // committed as v2 memories
	Unparsed  int // preserved verbatim under memories/team/unparsed/
	Pending   int // UNSURE.md entries routed to the review queue
}

// Run converts every v1 Markdown document in the clone.
//
// UNSURE.md entries go to the review queue rather than being promoted, because
// v1 could not decide where they belonged and neither should the migration.
func Run(s *store.Store, a *staging.Area) (Result, error) {
	var res Result
	clone := s.Layout().Clone

	docs, err := v1Documents(clone)
	if err != nil {
		return res, err
	}

	for _, doc := range docs {
		content, err := os.ReadFile(filepath.Join(clone, doc))
		if err != nil {
			return res, fmt.Errorf("migrate: read %q: %w", doc, err)
		}
		parsed, bad := ParseEntries(doc, string(content))

		unsure := strings.EqualFold(filepath.Base(doc), "UNSURE.md")
		for _, m := range parsed {
			if unsure {
				if err := a.Add(m); err != nil {
					return res, err
				}
				res.Pending++
				continue
			}
			if _, err := s.Put(m, "team-memory: migrate "+m.ID+" from "+doc); err != nil {
				return res, err
			}
			res.Converted++
		}

		for _, u := range bad {
			if err := preserve(clone, s, u); err != nil {
				return res, err
			}
			res.Unparsed++
		}
	}

	if res.Converted > 0 || res.Unparsed > 0 {
		if _, err := s.RegenerateIndex(); err != nil {
			return res, err
		}
	}
	return res, nil
}

// v1Documents lists Markdown files at the clone root and in legacy topic
// directories, excluding anything already under memories/ and excluding the
// derived INDEX.md.
func v1Documents(clone string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(clone, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(clone, path)
		if relErr != nil {
			return relErr
		}
		slashed := filepath.ToSlash(rel)

		if d.IsDir() {
			if slashed == ".git" || slashed == "memories" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || slashed == "INDEX.md" {
			return nil
		}
		out = append(out, slashed)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("migrate: scan clone: %w", err)
	}
	return out, nil
}

// preserve writes an unparsable block verbatim so nothing is ever lost. The
// filename is a hash of the raw block, which makes re-running the migration
// idempotent.
func preserve(clone string, s *store.Store, u Unparsed) error {
	sum := sha256.Sum256([]byte(u.Raw))
	name := hex.EncodeToString(sum[:])[:16] + ".md"
	rel := filepath.ToSlash(filepath.Join("memories", "team", "unparsed", name))
	abs := filepath.Join(clone, filepath.FromSlash(rel))

	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return fmt.Errorf("migrate: create unparsed directory: %w", err)
	}
	body := fmt.Sprintf("<!-- source: %s -->\n<!-- reason: %s -->\n\n%s\n", u.Source, u.Reason, u.Raw)
	if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
		return fmt.Errorf("migrate: write unparsed %q: %w", rel, err)
	}
	if _, err := s.PutRaw(rel, "team-memory: preserve unparsed entry from "+u.Source); err != nil {
		return err
	}
	return nil
}

var _ = memory.ScopeTeam // keep the memory import meaningful if preserve changes
```

- [ ] **Step 9: Add the `PutRaw` helper the runner needs**

`preserve` commits a file that is not a `memory.Memory`. Add this to
`mcp/store/commit.go`, below `Remove`:

```go
// PutRaw commits a file that already exists inside the clone. The path is
// repo-relative and passes through the same guard as every other write.
func (s *Store) PutRaw(repoPath, msg string) (plumbing.Hash, error) {
	abs := filepath.Join(s.layout.Clone, filepath.FromSlash(repoPath))
	if err := s.assertInsideClone(abs); err != nil {
		return plumbing.ZeroHash, err
	}
	return s.commitPath(abs, msg)
}
```

- [ ] **Step 10: Run the migration tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./migrate/ -v`
Expected: PASS — seven tests across both files.

- [ ] **Step 11: Run the whole suite**

Run: `cd mcp && CGO_ENABLED=0 go test ./...`
Expected: PASS. Pre-existing v1 packages must still pass — this plan adds
packages and does not modify v1 code.

- [ ] **Step 12: Commit**

```bash
git add mcp/migrate/migrate.go mcp/migrate/migrate_test.go mcp/store/commit.go
git commit -m "feat(migrate): convert v1 entries, route UNSURE to review, preserve failures"
```

---

## Definition of done for Plan 1

- [ ] `cd mcp && CGO_ENABLED=0 go test ./...` passes.
- [ ] `cd mcp && CGO_ENABLED=0 go build ./...` succeeds — the module remains cgo-free.
- [ ] `go list -m github.com/go-git/go-git/v5` reports v5.19.2 or later.
- [ ] `TestPutRefusesStagingPath` and `TestPutRefusesTraversalEscape` exist and pass.
- [ ] No call to `worktree.Add` exists anywhere outside `store.commitPath`.

## What Plan 1 deliberately does not do

These belong to later plans and must not be pulled forward:

- No HTTP server, no daemon, no MCP. That is Plan 2.
- No push to a remote. Approval-triggered push arrives with the review API in Plan 3.
- No LLM calls, no transcript parsing, no secret scanning. Plan 3.
- No CLI wiring for `team-memory migrate`. The `migrate.Run` function is
  complete and tested; the subcommand that calls it is added in Plan 2 alongside
  the rest of the command surface.
