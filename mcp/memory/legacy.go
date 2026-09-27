package memory

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	legacyEntryTS = regexp.MustCompile(`^Entry:\s*(\S+)\s*(?:—|-|\|)\s*(.*)$`)
	legacyField   = regexp.MustCompile(`^\*\*([A-Za-z ]+):\*\*\s*(.*)$`)
)

// ParseLegacy extracts entries from a pre-1.0 append-style file (GENERAL.md,
// DECISIONS.md, MEETINGS.md …). Every level-3 heading starts an entry. The
// result is read-only: IDs are derived from the file path and position.
func ParseLegacy(path, content string) []Memory {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	var out []Memory
	var cur *Memory
	var body []string
	flush := func() {
		if cur == nil {
			return
		}
		cur.Body = strings.TrimSpace(strings.Join(body, "\n"))
		if cur.Summary == "" {
			cur.Summary = firstLine(cur.Body)
		}
		out = append(out, *cur)
		cur, body = nil, nil
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "### ") {
			flush()
			heading := strings.TrimSpace(line[4:])
			m := Memory{
				ID:     fmt.Sprintf("legacy:%s#%d", path, len(out)+1),
				Title:  heading,
				Type:   legacyType(path, heading),
				Path:   path,
				Legacy: true,
				Source: "legacy",
			}
			if sm := legacyEntryTS.FindStringSubmatch(heading); sm != nil {
				if t, err := time.Parse(time.RFC3339, sm[1]); err == nil {
					m.Created = t
				}
				m.Title = strings.TrimSpace(sm[2])
			}
			for _, p := range []string{"Decision:", "Meeting:"} {
				if strings.HasPrefix(m.Title, p) {
					m.Title = strings.TrimSpace(strings.TrimPrefix(m.Title, p))
				}
			}
			cur = &m
			continue
		}
		if cur == nil {
			continue
		}
		if fm := legacyField.FindStringSubmatch(strings.TrimSpace(line)); fm != nil {
			key, val := strings.ToLower(fm[1]), strings.TrimSpace(fm[2])
			switch key {
			case "summary":
				cur.Summary = val
				continue
			case "tags":
				for _, t := range strings.FieldsFunc(val, func(r rune) bool { return r == ';' || r == ',' }) {
					if t = strings.TrimSpace(t); t != "" {
						cur.Tags = append(cur.Tags, t)
					}
				}
				continue
			case "date":
				if t, err := time.Parse("2006-01-02", val); err == nil {
					cur.Created = t
				}
			case "scope":
				if strings.HasPrefix(strings.ToLower(val), "personal") {
					cur.Scope = ScopePersonal
				}
				continue
			case "type", "source", "full":
				continue
			}
		}
		if strings.TrimSpace(line) == "---" {
			continue
		}
		body = append(body, line)
	}
	flush()
	return out
}

func legacyType(path, heading string) string {
	switch {
	case strings.HasPrefix(heading, "Decision:") || strings.Contains(strings.ToUpper(path), "DECISION"):
		return TypeDecision
	case strings.HasPrefix(heading, "Meeting:") || strings.Contains(strings.ToUpper(path), "MEETING"):
		return TypeMeeting
	case strings.Contains(strings.ToUpper(path), "REMINDER"):
		return TypeReminder
	}
	return TypeNote
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimLeft(s, "-*# ")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
