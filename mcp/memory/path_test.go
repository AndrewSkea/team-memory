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
