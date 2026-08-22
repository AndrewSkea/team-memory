package migrate

import (
	"strings"
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
	// v1's **Type:** has nowhere else to land in v2, so it is folded into
	// Tags rather than discarded (fix #2 from the task-9 review).
	if len(first.Tags) != 3 || first.Tags[0] != "storage" || first.Tags[2] != "decision" {
		t.Errorf("Tags = %v, want [storage git decision]", first.Tags)
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

// TestParseEntriesStripsLeadingBOM is the review's critical finding: a file
// starting with a UTF-8 byte-order mark hid its first "### Entry:" header
// from every prefix check, so the entry was neither parsed nor reported —
// it vanished, uncounted. It must now be parsed exactly as it would be
// without the BOM.
func TestParseEntriesStripsLeadingBOM(t *testing.T) {
	doc := "\uFEFF### Entry: 2026-01-01T00:00:00Z — BOM entry\n" +
		"**Scope:** shared\n**Full:**\nBody text.\n"
	got, bad := ParseEntries("BOM.md", doc)
	if len(bad) != 0 {
		t.Fatalf("unexpected unparsed entries: %+v", bad)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1 — the BOM must not hide the entry", len(got))
	}
}

// TestParseEntriesRecognisesIndentedHeader covers a header hand-indented
// under a list or by an editor's auto-indent. It must still be recognised as
// an entry, not silently swallowed into whatever came before it.
func TestParseEntriesRecognisesIndentedHeader(t *testing.T) {
	doc := "  ### Entry: 2026-01-01T00:00:00Z — Indented header\n" +
		"**Scope:** shared\n**Full:**\nBody text.\n"
	got, bad := ParseEntries("INDENTED.md", doc)
	if len(bad) != 0 {
		t.Fatalf("unexpected unparsed entries: %+v", bad)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
}

// TestParseEntriesRecognisesBlockquotedHeader covers an entry pasted into a
// reply and blockquoted with "> ". Its fields stay blockquoted too, so it is
// not expected to fully parse — but it must be counted as one block, either
// converted or reported as Unparsed, never silently dropped.
func TestParseEntriesRecognisesBlockquotedHeader(t *testing.T) {
	doc := "> ### Entry: 2026-01-01T00:00:00Z — Blockquoted header\n" +
		"> **Scope:** shared\n> **Full:**\n> Body text.\n"
	got, bad := ParseEntries("BLOCKQUOTED.md", doc)
	total := len(got) + len(bad)
	if total != 1 {
		t.Fatalf("accounted for %d blocks, want 1 — the header must be counted, not dropped", total)
	}
}

// TestParseEntriesDoesNotMergeAnIndentedHeaderIntoThePriorEntry is the
// review's worse sub-case: a well-formed entry followed by an indented
// header must produce two memories, not one with the second silently
// absorbed as body text of the first.
func TestParseEntriesDoesNotMergeAnIndentedHeaderIntoThePriorEntry(t *testing.T) {
	doc := "### Entry: 2026-01-01T00:00:00Z — First entry\n" +
		"**Scope:** shared\n**Type:** decision\n**Tags:** a\n" +
		"**Source:** session-1\n**Summary:** first\n**Bullets:** - one\n" +
		"**Full:**\nFirst body.\n\n" +
		"  ### Entry: 2026-01-02T00:00:00Z — Second entry, indented header\n" +
		"**Scope:** shared\n**Type:** decision\n**Tags:** b\n" +
		"**Source:** session-2\n**Summary:** second\n**Bullets:** - two\n" +
		"**Full:**\nSecond body.\n"

	got, bad := ParseEntries("MERGE.md", doc)
	if len(bad) != 0 {
		t.Fatalf("unexpected unparsed entries: %+v", bad)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 — the second entry must not merge into the first", len(got))
	}
	if strings.Contains(got[0].Body, "Second body.") {
		t.Errorf("first entry's body absorbed the second entry: %q", got[0].Body)
	}
	if got[0].SourceSession == got[1].SourceSession {
		t.Errorf("both entries report SourceSession %q — they should be distinct", got[0].SourceSession)
	}
}

// TestMapScopeDefaultsToTeam names the asymmetric-safety rule directly:
// anything that is not exactly "personal" must default to team, because
// misfiling a team memory as personal is a tidiness error but misfiling a
// personal memory as team exposes it to the whole team.
func TestMapScopeDefaultsToTeam(t *testing.T) {
	cases := map[string]string{
		"empty string":      "",
		"nonsense value":    "nonsense",
		"v1's own 'shared'": "shared",
	}
	for name, v1 := range cases {
		if got := mapScope(v1); got != memory.ScopeTeam {
			t.Errorf("%s: mapScope(%q) = %q, want team", name, v1, got)
		}
	}
}

// TestParseEntriesDefaultsMissingScopeFieldToTeam covers the same rule for
// an entry that omits **Scope:** entirely, not just an unrecognised value.
func TestParseEntriesDefaultsMissingScopeFieldToTeam(t *testing.T) {
	doc := "### Entry: 2026-01-01T00:00:00Z — No scope field at all\n" +
		"**Full:**\nBody text.\n"
	got, bad := ParseEntries("NOSCOPE.md", doc)
	if len(bad) != 0 {
		t.Fatalf("unexpected unparsed entries: %+v", bad)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got[0].Scope != memory.ScopeTeam {
		t.Errorf("Scope = %q, want team", got[0].Scope)
	}
}

// TestParseEntriesFoldsTypeIntoTags is fix #2 from the task-9 review: v1's
// **Type:** was parsed and then discarded. It must now survive as a tag.
func TestParseEntriesFoldsTypeIntoTags(t *testing.T) {
	doc := "### Entry: 2026-01-01T00:00:00Z — Has a type\n" +
		"**Scope:** shared\n**Type:** preference\n**Tags:** style\n" +
		"**Full:**\nBody text.\n"
	got, bad := ParseEntries("TYPE.md", doc)
	if len(bad) != 0 {
		t.Fatalf("unexpected unparsed entries: %+v", bad)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	found := false
	for _, tag := range got[0].Tags {
		if tag == "preference" {
			found = true
		}
	}
	if !found {
		t.Errorf("Tags = %v, want to contain %q", got[0].Tags, "preference")
	}
}

// TestParseEntriesPreservesLongTitleAsHeading is fix #3 from the task-9
// review: memory.Slugify caps the slug at 48 characters, so a long
// hand-written title is otherwise unrecoverable from the migrated file.
func TestParseEntriesPreservesLongTitleAsHeading(t *testing.T) {
	longTitle := strings.Repeat("long title words ", 30) // well over 48 chars
	longTitle = strings.TrimSpace(longTitle)
	doc := "### Entry: 2026-01-01T00:00:00Z — " + longTitle + "\n" +
		"**Scope:** shared\n**Full:**\nBody text.\n"
	got, bad := ParseEntries("LONGTITLE.md", doc)
	if len(bad) != 0 {
		t.Fatalf("unexpected unparsed entries: %+v", bad)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	wantHeading := "# " + longTitle
	if !strings.HasPrefix(got[0].Body, wantHeading) {
		t.Errorf("Body does not start with the full title as a heading.\nBody: %.80q...\nwant prefix: %.80q...",
			got[0].Body, wantHeading)
	}
	if len(got[0].Slug) > 48 {
		t.Errorf("Slug is %d chars, want capped at 48 (unchanged behaviour)", len(got[0].Slug))
	}
}

// TestParseEntriesDoesNotDuplicateTitleAlreadyPresent checks the
// heading-preservation fix does not double up a title that is already the
// first line of the body (e.g. a Summary that happens to repeat it).
func TestParseEntriesDoesNotDuplicateTitleAlreadyPresent(t *testing.T) {
	doc := "### Entry: 2026-01-01T00:00:00Z — Repeated title\n" +
		"**Scope:** shared\n**Summary:** Repeated title\n**Full:**\nBody text.\n"
	got, bad := ParseEntries("REPEAT.md", doc)
	if len(bad) != 0 {
		t.Fatalf("unexpected unparsed entries: %+v", bad)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if strings.Count(got[0].Body, "Repeated title") != 1 {
		t.Errorf("Body = %q, title should appear exactly once", got[0].Body)
	}
}
