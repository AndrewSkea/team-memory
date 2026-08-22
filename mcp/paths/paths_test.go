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
