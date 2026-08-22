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
