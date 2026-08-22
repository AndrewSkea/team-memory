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

func TestMarshalUnmarshalRoundTripBodyEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"no trailing newline", "line1"},
		{"single trailing newline", "line1\n"},
		{"blank line at end", "line1\n\n"},
		{"leading blank line", "\nHello"},
		{"crlf", "text\r\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := Memory{
				ID:      "a1b2c3d4e5f60718",
				Created: time.Date(2026, 8, 22, 14, 30, 0, 0, time.UTC),
				Author:  "andrew",
				Scope:   ScopeTeam,
				Body:    tc.body,
			}

			data, err := Marshal(in)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			out, err := Unmarshal(data)
			if err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			if out.Body != in.Body {
				t.Errorf("Body = %q, want %q", out.Body, in.Body)
			}
		})
	}
}

func TestUnmarshalNormalisesUnknownScopeToTeam(t *testing.T) {
	cases := map[string]Scope{
		"":           ScopeTeam,
		"team":       ScopeTeam,
		"Team":       ScopeTeam,
		"shared":     ScopeTeam,
		"nonsense":   ScopeTeam,
		"personal":   ScopePersonal,
		"Personal":   ScopePersonal,
		"PERSONAL":   ScopePersonal,
		" personal ": ScopePersonal,
	}
	for raw, want := range cases {
		data := []byte("---\nid: aaaa1111bbbb2222\ncreated: 2026-01-02T03:04:05Z\nauthor: andrew\nscope: \"" + raw + "\"\n---\n\nBody.\n")
		m, err := Unmarshal(data)
		if err != nil {
			t.Fatalf("Unmarshal with scope %q: %v", raw, err)
		}
		if m.Scope != want {
			t.Errorf("scope %q unmarshalled to %q, want %q", raw, m.Scope, want)
		}
	}
}

// TestUnmarshalledScopeAlwaysReachesAPath is the point of the normalisation:
// RepoPath hard-errors on an unknown scope, and readDirMemories drops a file
// whose Unmarshal fails, so an unrecognised scope word must be filed as team
// rather than being able to make a queued memory either unfilable or
// invisible. RepoPath keeps its error as defence in depth; nothing coming
// out of Unmarshal should be able to reach it.
func TestUnmarshalledScopeAlwaysReachesAPath(t *testing.T) {
	for _, raw := range []string{"", "team", "shared", "nonsense", "Personal"} {
		data := []byte("---\nid: aaaa1111bbbb2222\ncreated: 2026-01-02T03:04:05Z\nauthor: andrew\nscope: \"" + raw + "\"\nslug: a-thing\n---\n\nBody.\n")
		m, err := Unmarshal(data)
		if err != nil {
			t.Fatalf("Unmarshal with scope %q: %v", raw, err)
		}
		if _, err := RepoPath(m); err != nil {
			t.Errorf("RepoPath refused a memory unmarshalled with scope %q: %v", raw, err)
		}
	}
}

// TestRepoPathStillRefusesAnUnknownScope keeps the defence-in-depth error
// alive: a Memory built in code rather than parsed from disk can still carry
// a scope nothing knows how to file, and that must not be guessed at.
func TestRepoPathStillRefusesAnUnknownScope(t *testing.T) {
	m := Memory{ID: "aaaa1111bbbb2222", Scope: Scope("nonsense"), Slug: "a-thing"}
	if _, err := RepoPath(m); err == nil {
		t.Fatal("expected RepoPath to refuse an unknown scope")
	}
}
