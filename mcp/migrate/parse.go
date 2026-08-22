// Package migrate converts a v1 team-memory repository to the v2 layout.
//
// Migration is idempotent, re-runnable, and never rewrites history. An entry
// that cannot be parsed is preserved verbatim and reported rather than dropped.
package migrate

import (
	"fmt"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/memory"
)

const entryPrefix = "### Entry:"

// unknownAuthor is used for a v1 "personal" entry, because v1's rendered
// block (mcp/hook/render.go) carries no author field at all — only Scope,
// Type, Tags, Source (a session id, not a person), Summary, Bullets and
// Full. memory.RepoPath requires a non-empty Author for ScopePersonal, so
// without a placeholder every personal-scope v1 entry would fail to commit
// and the migration would violate "never drops an entry". The placeholder is
// deliberately not the session id: a session id is not a person, and filing
// it as though it were would misattribute the memory to a fabricated author
// rather than honestly recording that the author is unknown.
const unknownAuthor = "migrated"

// Unparsed is a v1 block that could not be converted.
type Unparsed struct {
	Source string
	Raw    string
	Reason string
}

// ParseEntries splits a v1 Markdown document into memories and failures.
func ParseEntries(source, content string) ([]memory.Memory, []Unparsed) {
	var (
		out []memory.Memory
		bad []Unparsed
	)
	for _, block := range splitBlocks(content) {
		m, err := parseBlock(block)
		if err != nil {
			bad = append(bad, Unparsed{Source: source, Raw: block, Reason: err.Error()})
			continue
		}
		out = append(out, m)
	}
	return out, bad
}

// splitBlocks returns each "### Entry:" block, header line included.
func splitBlocks(content string) []string {
	lines := strings.Split(content, "\n")
	var blocks []string
	var cur []string
	for _, line := range lines {
		if strings.HasPrefix(line, entryPrefix) {
			if len(cur) > 0 {
				blocks = append(blocks, strings.Join(cur, "\n"))
			}
			cur = []string{line}
			continue
		}
		if len(cur) > 0 {
			cur = append(cur, line)
		}
	}
	if len(cur) > 0 {
		blocks = append(blocks, strings.Join(cur, "\n"))
	}
	return blocks
}

func parseBlock(block string) (memory.Memory, error) {
	lines := strings.Split(block, "\n")
	header := strings.TrimSpace(strings.TrimPrefix(lines[0], entryPrefix))

	// "2026-05-12T09:30:00Z — Title". The separator is an em dash, matching
	// the format mcp/hook/render.go actually produces.
	ts, title, ok := strings.Cut(header, "—")
	if !ok {
		return memory.Memory{}, fmt.Errorf("header has no em-dash separator")
	}
	created, err := time.Parse(time.RFC3339, strings.TrimSpace(ts))
	if err != nil {
		return memory.Memory{}, fmt.Errorf("unparsable timestamp %q", strings.TrimSpace(ts))
	}

	fields := map[string]string{}
	var full []string
	inFull := false
	for _, line := range lines[1:] {
		if inFull {
			full = append(full, line)
			continue
		}
		if strings.TrimSpace(line) == "**Full:**" {
			inFull = true
			continue
		}
		if key, val, ok := cutField(line); ok {
			fields[key] = val
		}
	}

	scope := mapScope(fields["Scope"])
	m := memory.Memory{
		Created:       created,
		Slug:          memory.Slugify(strings.TrimSpace(title)),
		Scope:         scope,
		Tags:          splitTags(fields["Tags"]),
		SourceSession: fields["Source"],
		Body:          buildBody(fields["Summary"], fields["Bullets"], full),
	}
	if scope == memory.ScopePersonal {
		// v1 never recorded an author (see unknownAuthor's doc comment
		// above), so every migrated personal memory gets this placeholder
		// rather than being silently refiled as team, which would leak it.
		m.Author = unknownAuthor
	}
	if m.Body == "" {
		return memory.Memory{}, fmt.Errorf("entry has no body")
	}
	m.ID = memory.ComputeID(m)
	return m, nil
}

// cutField parses a "**Key:** value" line.
func cutField(line string) (key, val string, ok bool) {
	if !strings.HasPrefix(line, "**") {
		return "", "", false
	}
	rest := strings.TrimPrefix(line, "**")
	key, val, ok = strings.Cut(rest, ":**")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(val), true
}

// mapScope translates v1 scope words. v1 "shared" is v2 "team". Anything
// unrecognised — including empty, "shared", or garbage — defaults to team,
// per the spec's rule that ambiguous memories are team memories. Only an
// exact (case-insensitive) "personal" is ever treated as personal.
func mapScope(v1 string) memory.Scope {
	if strings.EqualFold(strings.TrimSpace(v1), "personal") {
		return memory.ScopePersonal
	}
	return memory.ScopeTeam
}

func splitTags(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// buildBody reassembles the v1 summary, bullets and full text into Markdown.
func buildBody(summary, bullets string, full []string) string {
	var b strings.Builder
	if summary != "" {
		b.WriteString(summary)
		b.WriteString("\n")
	}
	for _, item := range strings.Split(bullets, ";") {
		item = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(item), "-"))
		if item != "" {
			b.WriteString("\n- " + item)
		}
	}
	body := strings.TrimSpace(b.String())
	if rest := strings.TrimSpace(strings.Join(full, "\n")); rest != "" {
		if body != "" {
			body += "\n\n"
		}
		body += rest
	}
	return body
}
