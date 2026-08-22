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
