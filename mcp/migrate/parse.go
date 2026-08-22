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

// bom is the UTF-8 byte-order mark some editors and export tools prepend to
// a file. Left in place it hides the file's first "### Entry:" header from
// every prefix check below, so the header is neither parsed nor reported as
// unparsed; it simply vanishes. Stripping it once, up front, is cheaper and
// safer than teaching every downstream check about it.
const bom = "\uFEFF"

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
//
// A line only has to look like a header once its incidental decoration is
// stripped: leading whitespace (an entry hand-indented inside a list) and a
// leading blockquote marker (an entry pasted into a reply). Checking the raw
// line against entryPrefix alone would silently fold such a header into the
// body of whatever entry came before it, merging two memories into one with
// no error and no count anywhere. The blocks themselves still carry the
// original, undecorated line text: only the classification decision is
// normalised, not the content.
func splitBlocks(content string) []string {
	content = strings.TrimPrefix(content, bom)
	lines := strings.Split(content, "\n")
	var blocks []string
	var cur []string
	for _, line := range lines {
		if isEntryHeaderLine(line) {
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

// isEntryHeaderLine reports whether line is a "### Entry:" header once
// leading whitespace and blockquote markers are stripped.
func isEntryHeaderLine(line string) bool {
	return strings.HasPrefix(normalizeHeaderLine(line), entryPrefix)
}

// normalizeHeaderLine strips leading whitespace and any leading blockquote
// markers ("> ", possibly nested) so a decorated header line can be matched
// and parsed the same way as an undecorated one.
func normalizeHeaderLine(line string) string {
	for {
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, ">") {
			return trimmed
		}
		trimmed = strings.TrimPrefix(trimmed, ">")
		line = strings.TrimLeft(trimmed, " \t")
	}
}

func parseBlock(block string) (memory.Memory, error) {
	lines := strings.Split(block, "\n")
	header := strings.TrimSpace(strings.TrimPrefix(normalizeHeaderLine(lines[0]), entryPrefix))

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
	rawTitle := strings.TrimSpace(title)

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

	body := buildBody(fields["Summary"], fields["Bullets"], full)
	if body == "" {
		return memory.Memory{}, fmt.Errorf("entry has no body")
	}
	// The slug is capped at 48 characters (memory.Slugify), so a long
	// hand-written title is otherwise unrecoverable from the migrated file.
	// Carrying it as a heading keeps it, without adding a field to
	// memory.Memory that every other package would then have to know about.
	body = prependTitleHeading(rawTitle, body)

	scope := mapScope(fields["Scope"])
	tags := splitTags(fields["Tags"])
	// v1's Type (decision, preference, ...) had nowhere to land in v2 and was
	// silently discarded. Folding it into Tags needs no schema change; skip
	// it only when it is empty or already present.
	if typ := strings.TrimSpace(fields["Type"]); typ != "" && !containsFold(tags, typ) {
		tags = append(tags, typ)
	}

	m := memory.Memory{
		Created:       created,
		Slug:          memory.Slugify(rawTitle),
		Scope:         scope,
		Tags:          tags,
		SourceSession: fields["Source"],
		Body:          body,
	}
	if scope == memory.ScopePersonal {
		// v1 never recorded an author (see unknownAuthor's doc comment
		// above), so every migrated personal memory gets this placeholder
		// rather than being silently refiled as team, which would leak it.
		m.Author = unknownAuthor
	}
	m.ID = memory.ComputeID(m)
	return m, nil
}

// prependTitleHeading puts the original, unslugged title back as the body's
// first line, as a Markdown heading, unless it is already there (verbatim or
// already formatted as a heading) so a Summary that happens to repeat the
// title does not get doubled up.
func prependTitleHeading(title, body string) string {
	if title == "" {
		return body
	}
	firstLine := body
	if idx := strings.IndexByte(body, '\n'); idx >= 0 {
		firstLine = body[:idx]
	}
	firstLine = strings.TrimSpace(firstLine)
	if firstLine == title || firstLine == "# "+title {
		return body
	}
	return "# " + title + "\n\n" + body
}

// containsFold reports whether s is present in list, ignoring case.
func containsFold(list []string, s string) bool {
	for _, item := range list {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
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
//
// It delegates to memory.NormalizeScope so there is one rule rather than
// two: memory.Unmarshal reads these same words back off disk, and while the
// two disagreed the same byte was "safely defaulted" here and fatal there.
func mapScope(v1 string) memory.Scope {
	return memory.NormalizeScope(v1)
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
