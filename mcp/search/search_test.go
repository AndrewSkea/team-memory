package search

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/paths"
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

	var count int
	if err := ix.db.QueryRow(`SELECT count(*) FROM memories`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != len(sample()) {
		t.Errorf("row count after 3 rebuilds = %d, want %d (no duplication)", count, len(sample()))
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

// TestSearchMalformedQueriesNeverPanicOrError proves that raw FTS5 operator
// syntax and other adversarial input can never reach the query parser
// unescaped: every case below must return cleanly, with no error and no
// panic. None of these are expected to match anything in the sample set.
func TestSearchMalformedQueriesNeverPanicOrError(t *testing.T) {
	ix, _ := openTemp(t)
	_ = ix.Rebuild(sample())

	cases := []string{
		`"unbalanced`,          // unbalanced double quote
		`*`,                    // lone prefix-match operator with no token
		``,                     // empty string
		`   `,                  // whitespace only
		`!!!???...,,,;;;`,      // punctuation only
		`" OR "1"="1`,          // attempted injection style string
		`foo" -- bar`,          // quote followed by SQL-comment-looking text
		`AND OR NOT NEAR`,      // bare FTS5 operator keywords
		`(((unterminated`,      // unbalanced parens
	}
	for _, q := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Search(%q) panicked: %v", q, r)
				}
			}()
			if _, err := ix.Search(q, 10); err != nil {
				t.Fatalf("Search(%q) returned error, want clean result: %v", q, err)
			}
		}()
	}
}

// TestSearchDoesNotWriteInsideClone asserts the sqlite index — including any
// WAL/journal side files sqlite creates alongside it — is never written
// inside the git worktree. An index file committed into a shared repo would
// leak the content of personal-scope memories to everyone who clones it.
func TestSearchDoesNotWriteInsideClone(t *testing.T) {
	home := t.TempDir()
	layout, err := paths.New(home)
	if err != nil {
		t.Fatalf("paths.New: %v", err)
	}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	before, err := os.ReadDir(layout.Clone)
	if err != nil {
		t.Fatalf("ReadDir clone before: %v", err)
	}

	ix, err := Open(layout.Index)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer ix.Close()
	if err := ix.Rebuild(sample()); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if _, err := ix.Search("offline", 10); err != nil {
		t.Fatalf("Search: %v", err)
	}

	after, err := os.ReadDir(layout.Clone)
	if err != nil {
		t.Fatalf("ReadDir clone after: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("clone directory gained entries during indexing: before=%v after=%v", before, after)
	}

	inside, err := layout.IsInsideClone(layout.Index)
	if err != nil {
		t.Fatalf("IsInsideClone: %v", err)
	}
	if inside {
		t.Fatalf("layout.Index (%q) resolves inside layout.Clone (%q)", layout.Index, layout.Clone)
	}
}
