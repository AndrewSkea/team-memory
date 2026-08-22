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
