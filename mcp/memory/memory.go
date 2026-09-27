// Package memory defines the on-disk format of a team memory: one Markdown
// file per memory with YAML frontmatter, stored under memories/YYYY/MM/.
package memory

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Types a memory can have. Free-form types are tolerated on read.
const (
	TypeDecision   = "decision"
	TypeGotcha     = "gotcha"
	TypeHowTo      = "howto"
	TypeFact       = "fact"
	TypeConvention = "convention"
	TypeIncident   = "incident"
	TypeMeeting    = "meeting"
	TypeAction     = "action"
	TypeReminder   = "reminder"
	TypeIdea       = "idea"
	TypeNote       = "note"
)

var Types = []string{TypeDecision, TypeGotcha, TypeHowTo, TypeFact, TypeConvention,
	TypeIncident, TypeMeeting, TypeAction, TypeReminder, TypeIdea, TypeNote}

// Status values.
const (
	StatusActive     = "active"
	StatusSuperseded = "superseded"
	StatusRetracted  = "retracted"
)

// Scope values.
const (
	ScopeTeam     = "team"
	ScopePersonal = "personal" // recalled only for its author
)

// Memory is one unit of team knowledge.
type Memory struct {
	ID      string   `yaml:"id" json:"id"`
	Title   string   `yaml:"title" json:"title"`
	Type    string   `yaml:"type" json:"type"`
	Summary string   `yaml:"summary,omitempty" json:"summary,omitempty"`
	About   []string `yaml:"about,omitempty" json:"about,omitempty"` // systems/entities this is about — drives recall
	Tags    []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Scope   string   `yaml:"scope,omitempty" json:"scope,omitempty"`
	Status  string   `yaml:"status,omitempty" json:"status,omitempty"`

	// Provenance: where/how it was learned. Never used to filter recall.
	Author    string    `yaml:"author,omitempty" json:"author,omitempty"`
	LearnedIn string    `yaml:"learned_in,omitempty" json:"learned_in,omitempty"` // owner/repo of the working directory
	Source    string    `yaml:"source,omitempty" json:"source,omitempty"`         // session-end, precompact, ui, mcp, cli
	Created   time.Time `yaml:"created" json:"created"`
	Updated   time.Time `yaml:"updated,omitempty" json:"updated,omitzero"`

	Confidence   float64  `yaml:"confidence,omitempty" json:"confidence,omitempty"`
	Supersedes   []string `yaml:"supersedes,omitempty" json:"supersedes,omitempty"`
	SupersededBy string   `yaml:"superseded_by,omitempty" json:"superseded_by,omitempty"`
	StatusReason string   `yaml:"status_reason,omitempty" json:"status_reason,omitempty"`

	Body string `yaml:"-" json:"body"`
	Path string `yaml:"-" json:"path"` // repo-relative path; set when loaded or created
	// Legacy is true for entries parsed from pre-1.0 append-style files.
	// They are searchable but read-only.
	Legacy bool `yaml:"-" json:"legacy,omitempty"`
}

// Active reports whether the memory should be recalled.
func (m Memory) Active() bool { return m.Status == "" || m.Status == StatusActive }

// NewID returns a sortable, collision-resistant id like 20260927-153012-a1b2c3.
func NewID(now time.Time) string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

var idRe = regexp.MustCompile(`^\d{8}-\d{6}-[0-9a-f]{6}$`)

// ValidID reports whether id has the NewID shape.
func ValidID(id string) bool { return idRe.MatchString(id) }

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slug makes a filename-safe slug of at most 48 chars.
func Slug(s string) string {
	s = slugRe.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.Trim(s, "-")
	if len(s) > 48 {
		s = strings.TrimRight(s[:48], "-")
	}
	if s == "" {
		s = "memory"
	}
	return s
}

// PathFor returns the canonical repo path for a memory.
func PathFor(m Memory) string {
	t := m.Created.UTC()
	return fmt.Sprintf("memories/%04d/%02d/%s-%s.md", t.Year(), int(t.Month()), m.ID, Slug(m.Title))
}

// Render serialises a memory to Markdown with YAML frontmatter.
func Render(m Memory) (string, error) {
	if m.Status == StatusActive {
		m.Status = "" // omit the default
	}
	var fm bytes.Buffer
	enc := yaml.NewEncoder(&fm)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return "", err
	}
	_ = enc.Close()
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fm.Bytes())
	b.WriteString("---\n\n# ")
	b.WriteString(oneLine(m.Title))
	b.WriteString("\n\n")
	if body := strings.TrimSpace(m.Body); body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	} else if m.Summary != "" {
		b.WriteString(m.Summary)
		b.WriteString("\n")
	}
	return b.String(), nil
}

var errNoFrontmatter = errors.New("no frontmatter")

// Parse reads a memory file produced by Render (or hand-edited to match).
func Parse(path, content string) (Memory, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return Memory{}, errNoFrontmatter
	}
	rest := content[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return Memory{}, errNoFrontmatter
	}
	var m Memory
	if err := yaml.Unmarshal([]byte(rest[:end]), &m); err != nil {
		return Memory{}, fmt.Errorf("%s: frontmatter: %w", path, err)
	}
	body := rest[end+4:]
	if i := strings.Index(body, "\n"); i >= 0 {
		body = body[i+1:]
	} else {
		body = ""
	}
	body = strings.TrimSpace(body)
	// Drop the "# Title" heading Render writes.
	if strings.HasPrefix(body, "# ") {
		if i := strings.Index(body, "\n"); i >= 0 {
			if m.Title == "" {
				m.Title = strings.TrimSpace(body[2:i])
			}
			body = strings.TrimSpace(body[i+1:])
		} else {
			if m.Title == "" {
				m.Title = strings.TrimSpace(body[2:])
			}
			body = ""
		}
	}
	m.Body = body
	m.Path = path
	if m.ID == "" {
		return Memory{}, fmt.Errorf("%s: missing id", path)
	}
	if m.Type == "" {
		m.Type = TypeNote
	}
	return m, nil
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Text returns the searchable plain text of a memory.
func (m Memory) Text() string {
	return m.Title + "\n" + m.Summary + "\n" + m.Body
}
