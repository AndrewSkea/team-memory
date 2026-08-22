// Package memory defines the on-disk representation of a single memory:
// YAML frontmatter followed by a Markdown body.
package memory

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Scope decides which directory a memory is filed under. A personal memory
// still lives in the shared repository under memories/people/<author>/ — it is
// filed differently, not concealed.
type Scope string

const (
	ScopeTeam     Scope = "team"
	ScopePersonal Scope = "personal"
)

var delimiter = []byte("---\n")

// ErrNoFrontmatter is returned when a file does not open with a YAML
// frontmatter block.
var ErrNoFrontmatter = errors.New("memory: file has no frontmatter block")

// Memory is one atomic remembered fact.
type Memory struct {
	ID            string
	Created       time.Time
	Author        string
	Scope         Scope
	Tags          []string
	SourceSession string
	Confidence    float64
	Slug          string
	Body          string
}

// frontmatter mirrors Memory minus the Body, with explicit YAML keys so the
// on-disk format is stable regardless of Go field renames.
type frontmatter struct {
	ID            string    `yaml:"id"`
	Created       time.Time `yaml:"created"`
	Author        string    `yaml:"author,omitempty"`
	Scope         Scope     `yaml:"scope,omitempty"`
	Tags          []string  `yaml:"tags,omitempty"`
	SourceSession string    `yaml:"source_session,omitempty"`
	Confidence    float64   `yaml:"confidence,omitempty"`
	Slug          string    `yaml:"slug,omitempty"`
}

// Marshal renders a Memory as frontmatter plus body.
func Marshal(m Memory) ([]byte, error) {
	fm := frontmatter{
		ID:            m.ID,
		Created:       m.Created.UTC(),
		Author:        m.Author,
		Scope:         m.Scope,
		Tags:          m.Tags,
		SourceSession: m.SourceSession,
		Confidence:    m.Confidence,
		Slug:          m.Slug,
	}
	y, err := yaml.Marshal(fm)
	if err != nil {
		return nil, fmt.Errorf("memory: marshal frontmatter: %w", err)
	}

	var buf bytes.Buffer
	buf.Write(delimiter)
	buf.Write(y)
	buf.Write(delimiter)
	buf.WriteString(m.Body)
	if len(m.Body) > 0 && m.Body[len(m.Body)-1] != '\n' {
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// Unmarshal parses frontmatter plus body back into a Memory. A memory with no
// explicit scope defaults to team, matching the spec's rule that ambiguous
// memories are team memories.
func Unmarshal(data []byte) (Memory, error) {
	if !bytes.HasPrefix(data, delimiter) {
		return Memory{}, ErrNoFrontmatter
	}
	rest := data[len(delimiter):]
	end := bytes.Index(rest, delimiter)
	if end < 0 {
		return Memory{}, ErrNoFrontmatter
	}

	var fm frontmatter
	if err := yaml.Unmarshal(rest[:end], &fm); err != nil {
		return Memory{}, fmt.Errorf("memory: parse frontmatter: %w", err)
	}
	if fm.Scope == "" {
		fm.Scope = ScopeTeam
	}

	body := string(bytes.TrimPrefix(rest[end+len(delimiter):], []byte("\n")))
	body = strings.TrimSuffix(body, "\n")
	return Memory{
		ID:            fm.ID,
		Created:       fm.Created,
		Author:        fm.Author,
		Scope:         fm.Scope,
		Tags:          fm.Tags,
		SourceSession: fm.SourceSession,
		Confidence:    fm.Confidence,
		Slug:          fm.Slug,
		Body:          body,
	}, nil
}
