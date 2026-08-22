package store

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	assertNothingStaged(t, s)
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
	assertNothingStaged(t, s)
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

// ---------------------------------------------------------------------------
// Adversarial tests against the guard itself.
// ---------------------------------------------------------------------------

// assertNothingStaged proves a refusal left no residue in the git index: a
// refused write must not have half-staged anything.
func assertNothingStaged(t *testing.T, s *Store) {
	t.Helper()
	wt, err := s.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	status, err := wt.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.IsClean() {
		t.Errorf("index/worktree should be untouched after a refusal, got %v", status)
	}
}

// linkDir creates a directory link at link pointing at target. On Windows an
// unprivileged process cannot create a symlink, so fall back to a directory
// junction, which filepath.EvalSymlinks resolves the same way. Without the
// fallback the symlink attack would be silently skipped on the platform where
// this code is developed.
func linkDir(target, link string) error {
	err := os.Symlink(target, link)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	out, mkErr := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if mkErr != nil {
		return fmt.Errorf("os.Symlink: %v; mklink /J: %v: %s", err, mkErr, out)
	}
	return nil
}

// PERMANENT REGRESSION TEST. Do not delete, skip, or weaken.
//
// A link planted inside the clone that points out to staging must not launder
// staging content into the repository. paths owns the primitive regression
// test for this; the funnel gets its own because the funnel is what production
// calls. This test fails rather than skips when the hostile condition cannot be
// built: a security test that reports green without having run is worse than
// no test at all.
func TestCommitPathRefusesSymlinkOrJunctionOutOfClone(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	link := filepath.Join(l.Clone, "shortcut")
	if err := linkDir(l.Pending, link); err != nil {
		t.Fatalf("cannot create a directory link, so the privacy guarantee is "+
			"UNVERIFIED on this machine: %v", err)
	}
	leak := filepath.Join(l.Pending, "unreviewed.md")
	if err := os.WriteFile(leak, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	via := filepath.Join(link, "unreviewed.md")
	if _, err := s.commitPath(via, "should never happen"); !errors.Is(err, ErrOutsideClone) {
		t.Fatalf("symlinked staging path returned %v, want ErrOutsideClone", err)
	}
}

// A sibling directory whose name merely starts with the clone name is not
// inside the clone. A naive strings.HasPrefix without a separator admits it.
func TestCommitPathRefusesCloneNamePrefixSibling(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	evil := l.Clone + "-evil"
	if err := os.MkdirAll(evil, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	victim := filepath.Join(evil, "leak.md")
	if err := os.WriteFile(victim, []byte("leak"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := s.commitPath(victim, "should never happen"); !errors.Is(err, ErrOutsideClone) {
		t.Fatalf("clone-name-prefix sibling returned %v, want ErrOutsideClone", err)
	}
}

// An absolute path somewhere else entirely on the filesystem.
func TestCommitPathRefusesUnrelatedAbsolutePath(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	elsewhere := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(elsewhere, []byte("leak"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := s.commitPath(elsewhere, "should never happen"); !errors.Is(err, ErrOutsideClone) {
		t.Fatalf("unrelated absolute path returned %v, want ErrOutsideClone", err)
	}
}

// The staging directory itself, and the home root that contains both trees.
func TestCommitPathRefusesStagingAndHomeRoots(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, p := range []string{l.Staging, l.Pending, l.Bin, l.Home, l.TokenFile, l.Index} {
		if _, err := s.commitPath(p, "should never happen"); !errors.Is(err, ErrOutsideClone) {
			t.Errorf("commitPath(%q) = %v, want ErrOutsideClone", p, err)
		}
	}
}

// Remove must refuse before it deletes: a refused Remove leaves the target
// file untouched on disk.
func TestRemoveRefusesEscapeWithoutDeleting(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	leak := filepath.Join(l.Pending, "unreviewed.md")
	if err := os.WriteFile(leak, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := s.Remove("../staging/pending/unreviewed.md", "nope"); !errors.Is(err, ErrOutsideClone) {
		t.Fatalf("Remove escape returned %v, want ErrOutsideClone", err)
	}
	if _, err := os.Stat(leak); err != nil {
		t.Errorf("refused Remove must not delete the file: %v", err)
	}
}

// A ".." sequence that resolves back inside the clone is legitimate and must
// be allowed: the guard resolves paths, it does not reject them lexically.
func TestCommitPathAllowsTraversalThatLandsInsideClone(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := filepath.Join(l.Clone, "memories", "team")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := filepath.Join(l.Clone, "memories", "team", "..", "team", "note.md")
	if err := os.WriteFile(filepath.Clean(target), []byte("---\nid: x\n---\n\nhi\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := s.commitPath(target, "legit"); err != nil {
		t.Fatalf("traversal landing inside the clone should be allowed, got %v", err)
	}
}

// Git's own metadata is inside the clone but must never be staged.
func TestCommitPathRefusesGitInternalPath(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p := filepath.Join(l.Clone, ".git", "config")
	if _, err := s.commitPath(p, "should never happen"); !errors.Is(err, ErrGitInternal) {
		t.Fatalf("commitPath(.git/config) = %v, want ErrGitInternal", err)
	}
}

// On a case-insensitive filesystem, ".GIT" is git metadata too.
func TestCommitPathRefusesGitInternalPathCaseInsensitive(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p := filepath.Join(l.Clone, ".GIT", "config")
	if _, err := s.commitPath(p, "should never happen"); !errors.Is(err, ErrGitInternal) {
		t.Fatalf("commitPath(.GIT/config) = %v, want ErrGitInternal", err)
	}
}

// Put on a personal-scope memory still files inside the clone, under people/.
func TestPutPersonalScopeStaysInsideClone(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	m := newMemory("A personal note.")
	m.Scope = memory.ScopePersonal
	m.ID = memory.ComputeID(m)
	if _, err := s.Put(m, "add personal"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rel, _ := memory.RepoPath(m)
	if !strings.HasPrefix(rel, "memories/people/andrew/") {
		t.Errorf("unexpected personal path %q", rel)
	}
	if _, err := os.Stat(filepath.Join(l.Clone, filepath.FromSlash(rel))); err != nil {
		t.Errorf("personal memory not written: %v", err)
	}
}

// Two sequential Puts produce two commits and leave the worktree clean.
func TestPutIsRepeatable(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	h1, err := s.Put(newMemory("first"), "one")
	if err != nil {
		t.Fatalf("Put one: %v", err)
	}
	h2, err := s.Put(newMemory("second"), "two")
	if err != nil {
		t.Fatalf("Put two: %v", err)
	}
	if h1 == h2 {
		t.Errorf("two puts produced the same commit %s", h1)
	}
	assertNothingStaged(t, s)
}

// The clone root itself is refused. It resolves "inside" the clone, but
// staging it means worktree.Add("."): every file in the worktree in a single
// commit. Every commit this store makes is exactly one memory file.
func TestCommitPathRefusesCloneRoot(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.Put(newMemory("a real memory"), "add"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	for _, p := range []string{l.Clone, l.Clone + string(filepath.Separator), filepath.Join(l.Clone, ".")} {
		if _, err := s.commitPath(p, "should never happen"); !errors.Is(err, ErrOutsideClone) {
			t.Errorf("commitPath(%q) = %v, want ErrOutsideClone", p, err)
		}
	}
}

// Remove refuses the clone root too, and does not delete anything trying.
func TestRemoveRefusesCloneRoot(t *testing.T) {
	l := newLayout(t)
	s, err := Open(l)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.Remove(".", "should never happen"); !errors.Is(err, ErrOutsideClone) {
		t.Fatalf("Remove(\".\") = %v, want ErrOutsideClone", err)
	}
	if _, err := os.Stat(l.Clone); err != nil {
		t.Errorf("clone must still exist: %v", err)
	}
}
