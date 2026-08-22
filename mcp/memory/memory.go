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

// NormalizeScope maps a scope word read off disk onto one of the two scopes
// that actually exist. A case-insensitive "personal" is personal; EVERYTHING
// else - empty, v1's "shared", a typo, garbage - is team, per the rule that
// an ambiguous memory is a team memory.
//
// It deliberately does not report an error. Unmarshal is what parses queued
// files, and staging's readDirMemories drops any file Unmarshal rejects, so
// making an unrecognised scope fatal would silently delete a memory from the
// review queue over one typo. Filing it as team is the honest failure: the
// memory is still there, still reviewable, and a reviewer can correct it.
func NormalizeScope(v string) Scope {
	if strings.EqualFold(strings.TrimSpace(v), string(ScopePersonal)) {
		return ScopePersonal
	}
	return ScopeTeam
}

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
	buf.WriteByte('\n')
	buf.WriteString(m.Body)
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// Unmarshal parses frontmatter plus body back into a Memory. The scope is
// normalised through NormalizeScope, so an absent, mis-cased or unrecognised
// scope word becomes team rather than being passed through verbatim for
// RepoPath to hard-error on later. Before this, migrate defaulted such a word
// to team while memory treated it as fatal: the same byte on disk was safe in
// one package and unfilable in the other.
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
	fm.Scope = NormalizeScope(string(fm.Scope))

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
