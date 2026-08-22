package paths

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// linkDir creates a directory link at link pointing at target. On Windows an
// unprivileged process cannot create a symlink, so fall back to a directory
// junction, which is the more dangerous case anyway: filepath.EvalSymlinks
// does not follow a junction.
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
// A link planted inside the clone that points out to staging must not be able
// to launder unreviewed content into a shareable commit. This test does not
// skip when it cannot create the hostile condition: a security test that
// reports green without having run is worse than no test at all.
func TestIsInsideCloneRefusesLinkedComponent(t *testing.T) {
	l, _ := New(t.TempDir())
	if err := l.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	link := filepath.Join(l.Clone, "shortcut")
	if err := linkDir(l.Pending, link); err != nil {
		t.Fatalf("cannot create a directory link, so the clone-boundary guarantee "+
			"is UNVERIFIED on this machine: %v", err)
	}
	leak := filepath.Join(l.Pending, "unreviewed.md")
	if err := os.WriteFile(leak, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	via := filepath.Join(link, "unreviewed.md")
	ok, err := l.IsInsideClone(via)
	if ok {
		t.Errorf("%q reaches staging through a link and must NOT be inside the clone", via)
	}
	if !errors.Is(err, ErrLinkedComponent) {
		t.Errorf("err = %v, want ErrLinkedComponent", err)
	}
}

// The link itself, not just a path below it, is refused.
func TestIsInsideCloneRefusesTheLinkItself(t *testing.T) {
	l, _ := New(t.TempDir())
	if err := l.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	link := filepath.Join(l.Clone, "shortcut")
	if err := linkDir(l.Pending, link); err != nil {
		t.Fatalf("cannot create a directory link, so the clone-boundary guarantee "+
			"is UNVERIFIED on this machine: %v", err)
	}
	ok, err := l.IsInsideClone(link)
	if ok || !errors.Is(err, ErrLinkedComponent) {
		t.Errorf("IsInsideClone(link) = %v, %v; want false, ErrLinkedComponent", ok, err)
	}
}

// An ordinary not-yet-existing path below a real directory stays inside, so
// the link check does not break the normal write path.
func TestIsInsideCloneAllowsUncreatedDescendant(t *testing.T) {
	l, _ := New(t.TempDir())
	if err := l.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	p := filepath.Join(l.Clone, "memories", "team", "2026", "08", "x.md")
	ok, err := l.IsInsideClone(p)
	if err != nil {
		t.Fatalf("IsInsideClone: %v", err)
	}
	if !ok {
		t.Errorf("%q should be inside the clone", p)
	}
}

// The clone root itself still reports inside. Refusing it is the store's rule,
// not this primitive's.
func TestIsInsideCloneAcceptsCloneRoot(t *testing.T) {
	l, _ := New(t.TempDir())
	if err := l.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	ok, err := l.IsInsideClone(l.Clone)
	if err != nil {
		t.Fatalf("IsInsideClone: %v", err)
	}
	if !ok {
		t.Error("the clone root should report as inside the clone")
	}
}
