// Package entities is the team's self-building catalogue of systems,
// services, libraries and teams that memories can be "about". It lives in
// .team-memory/entities.yml in the memory repo. Recall matches the current
// context (prompt, repo dependencies, env var names) against these names and
// aliases, so knowledge learned in one repo reaches anyone touching the same
// system from another.
package entities

import (
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const Path = ".team-memory/entities.yml"

type Entity struct {
	Name        string   `yaml:"name" json:"name"`
	Aliases     []string `yaml:"aliases,omitempty" json:"aliases,omitempty"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
}

type Registry struct {
	Entities []Entity `yaml:"entities"`
}

func Parse(data []byte) (Registry, error) {
	var r Registry
	if len(strings.TrimSpace(string(data))) == 0 {
		return r, nil
	}
	err := yaml.Unmarshal(data, &r)
	return r, err
}

func (r Registry) Render() string {
	sort.Slice(r.Entities, func(i, j int) bool { return r.Entities[i].Name < r.Entities[j].Name })
	out, _ := yaml.Marshal(r)
	return "# Systems, services and libraries team memories can be about.\n" +
		"# Maintained automatically by team-memory; edit freely (add aliases!).\n" + string(out)
}

// Canonical returns the canonical name for a name or alias, if known.
func (r Registry) Canonical(name string) (string, bool) {
	n := Normalize(name)
	for _, e := range r.Entities {
		if Normalize(e.Name) == n {
			return e.Name, true
		}
		for _, a := range e.Aliases {
			if Normalize(a) == n {
				return e.Name, true
			}
		}
	}
	return "", false
}

// Merge adds new entities or new aliases. It reports whether anything changed.
func (r *Registry) Merge(in []Entity) bool {
	changed := false
	for _, e := range in {
		e.Name = CanonicalName(e.Name)
		if e.Name == "" {
			continue
		}
		idx := -1
		if c, ok := r.Canonical(e.Name); ok {
			for i := range r.Entities {
				if r.Entities[i].Name == c {
					idx = i
				}
			}
		}
		if idx < 0 {
			r.Entities = append(r.Entities, Entity{Name: e.Name, Description: e.Description})
			idx = len(r.Entities) - 1
			changed = true
		}
		ex := &r.Entities[idx]
		if ex.Description == "" && e.Description != "" {
			ex.Description = e.Description
			changed = true
		}
		for _, a := range e.Aliases {
			a = strings.TrimSpace(a)
			if a == "" || Normalize(a) == Normalize(ex.Name) {
				continue
			}
			if _, known := r.Canonical(a); known {
				continue
			}
			ex.Aliases = append(ex.Aliases, a)
			changed = true
		}
	}
	return changed
}

// CanonicalName turns a free-form name into the registry's kebab-case form.
func CanonicalName(s string) string {
	return strings.ReplaceAll(Normalize(s), " ", "-")
}

// Normalize lowercases and turns separators into single spaces so that
// "Payments_API", "payments-api" and "payments api" compare equal.
func Normalize(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// Matcher finds entity mentions in text.
type Matcher struct {
	phrases []phrase
}

type phrase struct {
	norm string
	name string
}

// NewMatcher indexes all names and aliases. extra adds implicit entities
// (e.g. names used in memories' `about` that are not in the registry).
func NewMatcher(r Registry, extra []string) *Matcher {
	m := &Matcher{}
	seen := map[string]bool{}
	add := func(p, name string) {
		n := Normalize(p)
		if len(n) < 3 || seen[n+"\x00"+name] {
			return
		}
		seen[n+"\x00"+name] = true
		m.phrases = append(m.phrases, phrase{n, name})
	}
	for _, e := range r.Entities {
		add(e.Name, e.Name)
		for _, a := range e.Aliases {
			add(a, e.Name)
		}
	}
	for _, x := range extra {
		if _, ok := r.Canonical(x); !ok {
			add(x, CanonicalName(x))
		}
	}
	return m
}

// Match returns canonical names of entities mentioned in text.
func (m *Matcher) Match(text string) []string {
	hay := " " + Normalize(text) + " "
	found := map[string]bool{}
	for _, p := range m.phrases {
		if strings.Contains(hay, " "+p.norm+" ") {
			found[p.name] = true
		}
	}
	out := make([]string, 0, len(found))
	for n := range found {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
