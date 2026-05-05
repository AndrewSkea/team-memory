package hook

import (
	"fmt"
	"strings"
)

type Entry struct {
	Timestamp  string
	ShortTitle string
	Scope      string
	Type       string
	Tags       string
	Source     string
	Summary    string
	Bullets    []string
	Full       string
}

func RenderEntry(e Entry) string {
	bullets := make([]string, len(e.Bullets))
	for i, b := range e.Bullets {
		bullets[i] = "- " + b
	}
	bulletsStr := strings.Join(bullets, " ; ")
	lines := []string{
		fmt.Sprintf("### Entry: %s — %s", e.Timestamp, e.ShortTitle),
		fmt.Sprintf("**Scope:** %s", e.Scope),
		fmt.Sprintf("**Type:** %s", e.Type),
		fmt.Sprintf("**Tags:** %s", e.Tags),
		fmt.Sprintf("**Source:** %s", e.Source),
		fmt.Sprintf("**Summary:** %s", e.Summary),
		fmt.Sprintf("**Bullets:** %s", bulletsStr),
		"**Full:**",
		e.Full,
		"",
	}
	return strings.Join(lines, "\n")
}
