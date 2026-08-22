package staging

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/paths"
	"github.com/AndrewSkea/team-memory/mcp/store"
)

// hostilePending is a pending file whose FILENAME is a well-formed id but
// whose frontmatter claims a traversing one. Frontmatter is file content and
// therefore untrusted; the filename the directory listing produced is not.
const hostilePending = `---
id: ../../../../evil
created: 2026-01-02T03:04:05Z
scope: team
slug: pwn
---

Hostile body.
`

// TestTakeThenPutCannotEscapeMemoriesDir is the end-to-end proof for the
// traversal hole: a pending file carrying a traversing frontmatter id must
// not be able to make store.Put write or commit anything outside memories/.
func TestTakeThenPutCannotEscapeMemoriesDir(t *testing.T) {
	home := t.TempDir()
	l, err := paths.New(home)
	if err != nil {
		t.Fatalf("paths.New: %v", err)
	}
	a, err := New(l)
	if err != nil {
		t.Fatalf("staging.New: %v", err)
	}
	s, err := store.Open(l)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	const id = "aaaa1111bbbb2222"
	if err := os.WriteFile(filepath.Join(l.Pending, id+".md"), []byte(hostilePending), 0o600); err != nil {
		t.Fatalf("plant pending: %v", err)
	}

	// The flow the approval path will use. Either step may refuse; what must
	// never happen is a file landing outside memories/.
	m, takeErr := a.Take(id)
	if takeErr == nil {
		if m.ID != id {
			t.Errorf("Take returned ID %q, want the filename-derived %q", m.ID, id)
		}
		if _, err := s.Put(m, "approve "+id); err != nil {
			t.Logf("Put refused (acceptable): %v", err)
		}
	} else {
		t.Logf("Take refused (acceptable): %v", takeErr)
	}

	assertNothingOutsideMemories(t, l.Clone)
}

// assertNothingOutsideMemories walks the clone and, separately, asks git what
// it has tracked. A file can escape either way: written to disk outside
// memories/, or staged into the index from outside it.
func assertNothingOutsideMemories(t *testing.T, clone string) {
	t.Helper()
	err := filepath.WalkDir(clone, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(clone, path)
		if relErr != nil {
			return relErr
		}
		slashed := filepath.ToSlash(rel)
		if slashed == "." {
			return nil
		}
		if slashed == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(slashed, ".md") {
			return nil
		}
		if !strings.HasPrefix(slashed, "memories/") {
			t.Errorf("file landed outside memories/: %s", slashed)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk clone: %v", err)
	}

	cmd := exec.Command("git", "ls-files")
	cmd.Dir = clone
	out, lsErr := cmd.Output()
	if lsErr != nil {
		t.Fatalf("git ls-files: %v", lsErr)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "memories/") {
			t.Errorf("git tracks a file outside memories/: %s", line)
		}
	}
}

// unusablePending is a well-formed pending file the store will refuse: a
// personal-scope memory with no author has no path to be filed at.
const unusablePending = `---
id: cccc3333dddd4444
created: 2026-01-02T03:04:05Z
scope: personal
slug: nowhere
---

Body the store cannot file.
`

// TestPutRefusalDoesNotDestroyThePendingMemory is the regression proof for
// the old destructive Take: it deleted the pending file and returned, and
// the caller then discovered store.Put would not accept the memory. There
// was no compensating re-add anywhere, so the memory ended up in neither
// pending, the store, nor the bin - simply gone.
func TestPutRefusalDoesNotDestroyThePendingMemory(t *testing.T) {
	home := t.TempDir()
	l, err := paths.New(home)
	if err != nil {
		t.Fatalf("paths.New: %v", err)
	}
	a, err := New(l)
	if err != nil {
		t.Fatalf("staging.New: %v", err)
	}
	s, err := store.Open(l)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	const id = "cccc3333dddd4444"
	if err := os.WriteFile(filepath.Join(l.Pending, id+".md"), []byte(unusablePending), 0o600); err != nil {
		t.Fatalf("plant pending: %v", err)
	}

	m, err := a.Take(id)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if _, err := s.Put(m, "approve "+id); err == nil {
		t.Fatal("expected store.Put to refuse a personal memory with no author")
	}
	if err := a.Abort(id); err != nil {
		t.Fatalf("Abort: %v", err)
	}

	pending, err := a.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("the refused memory must still be queued for review, got %v", pending)
	}
	binned, err := a.Binned()
	if err != nil {
		t.Fatalf("Binned: %v", err)
	}
	if len(binned) != 0 {
		t.Fatalf("nothing should have been binned, got %d", len(binned))
	}
}
