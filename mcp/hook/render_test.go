package hook_test

import (
	"strings"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/hook"
)

func TestRenderEntry(t *testing.T) {
	e := hook.Entry{
		Timestamp:  "2026-05-05T12:00:00Z",
		ShortTitle: "test session",
		Scope:      "Team",
		Type:       "General",
		Tags:       "go;testing",
		Source:     "Stop",
		Summary:    "Implemented something",
		Bullets:    []string{"did A", "did B"},
		Full:       "Implemented something",
	}
	got := hook.RenderEntry(e)
	want := []string{
		"### Entry: 2026-05-05T12:00:00Z — test session",
		"**Scope:** Team",
		"**Type:** General",
		"**Tags:** go;testing",
		"**Source:** Stop",
		"**Summary:** Implemented something",
		"**Bullets:** - did A ; - did B",
		"**Full:**",
		"Implemented something",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func TestRenderEntryNoBullets(t *testing.T) {
	e := hook.Entry{
		Timestamp:  "2026-05-05T12:00:00Z",
		ShortTitle: "empty",
		Source:     "PreCompact",
	}
	got := hook.RenderEntry(e)
	if !strings.Contains(got, "**Bullets:**") {
		t.Error("missing Bullets line")
	}
}
