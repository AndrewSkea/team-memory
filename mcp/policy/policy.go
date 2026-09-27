// Package policy decides what happens to an automatically captured memory:
// publish it to the team, hold it for the author's review, or drop it.
//
// Users pick a capture mode; the team can set a default and a minimum
// strictness in .team-memory/policy.yml in the memory repo. A user may be
// stricter than the team minimum, never looser.
package policy

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const Path = ".team-memory/policy.yml"

type Mode string

const (
	Auto          Mode = "auto"           // publish everything shareable immediately
	AI            Mode = "ai"             // publish confident items, hold the rest for review
	ReviewTimeout Mode = "review-timeout" // hold everything; auto-publish after N days
	Review        Mode = "review"         // hold everything until approved
	Manual        Mode = "manual"         // no automatic capture
)

// Modes in increasing strictness.
var Modes = []Mode{Auto, AI, ReviewTimeout, Review, Manual}

var Descriptions = map[Mode]string{
	Auto:          "Publish everything the AI extracts immediately (personal/sensitive items are never shared)",
	AI:            "Confident, clearly shareable items publish; anything uncertain waits for your review",
	ReviewTimeout: "Everything waits for review, but publishes automatically after a few days unless you reject it",
	Review:        "Everything waits for your approval",
	Manual:        "No automatic capture — only what you add yourself",
}

func (m Mode) rank() int {
	for i, x := range Modes {
		if x == m {
			return i
		}
	}
	return -1
}

func ParseMode(s string) (Mode, error) {
	m := Mode(strings.ToLower(strings.TrimSpace(s)))
	if m.rank() < 0 {
		return "", fmt.Errorf("unknown mode %q (want one of auto, ai, review-timeout, review, manual)", s)
	}
	return m, nil
}

// Policy is the team-level configuration.
type Policy struct {
	DefaultMode       Mode     `yaml:"default_mode"`
	MinimumMode       Mode     `yaml:"minimum_mode"`
	ReviewTimeoutDays int      `yaml:"review_timeout_days"`
	AlwaysReviewTypes []string `yaml:"always_review_types"`
	MinConfidence     float64  `yaml:"min_confidence"`
	// OutsideOrg controls sessions in repos not owned by the team's GitHub
	// owner(s): "review" (hold), "skip" (don't capture) or "allow".
	OutsideOrg string `yaml:"outside_org"`
	// Orgs lists extra GitHub owners treated as the team's own. The memory
	// repo's owner is always included.
	Orgs []string `yaml:"orgs"`
}

func Default() Policy {
	return Policy{
		DefaultMode:       AI,
		MinimumMode:       Auto,
		ReviewTimeoutDays: 3,
		MinConfidence:     0.75,
		OutsideOrg:        "review",
	}
}

// Parse reads policy.yml, filling unset fields with defaults.
func Parse(data []byte) (Policy, error) {
	p := Default()
	if len(strings.TrimSpace(string(data))) == 0 {
		return p, nil
	}
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Default(), err
	}
	d := Default()
	if p.DefaultMode.rank() < 0 {
		p.DefaultMode = d.DefaultMode
	}
	if p.MinimumMode.rank() < 0 {
		p.MinimumMode = d.MinimumMode
	}
	if p.ReviewTimeoutDays <= 0 {
		p.ReviewTimeoutDays = d.ReviewTimeoutDays
	}
	if p.MinConfidence <= 0 || p.MinConfidence > 1 {
		p.MinConfidence = d.MinConfidence
	}
	switch p.OutsideOrg {
	case "review", "skip", "allow":
	default:
		p.OutsideOrg = d.OutsideOrg
	}
	return p, nil
}

func (p Policy) Render() string {
	out, _ := yaml.Marshal(p)
	return "# team-memory capture policy. Modes: auto, ai, review-timeout, review, manual\n" +
		"# default_mode applies to users who haven't picked one; minimum_mode is the\n" +
		"# least strict mode anyone may use.\n" + string(out)
}

// Effective returns the mode in force for a user who chose userMode ("" =
// team default), clamped to the team minimum.
func (p Policy) Effective(userMode string) Mode {
	m := p.DefaultMode
	if um, err := ParseMode(userMode); err == nil && userMode != "" {
		m = um
	}
	if m.rank() < p.MinimumMode.rank() {
		m = p.MinimumMode
	}
	return m
}

// Allowed lists the modes a user may choose.
func (p Policy) Allowed() []Mode {
	var out []Mode
	for _, m := range Modes {
		if m.rank() >= p.MinimumMode.rank() {
			out = append(out, m)
		}
	}
	return out
}

// Candidate is what the extractor proposes.
type Candidate struct {
	Type        string
	Sensitivity string // shareable | personal | sensitive
	Confidence  float64
	Contradicts bool // supersedes or conflicts with an existing memory
	Redacted    bool // secrets were stripped
}

type Action string

const (
	Publish Action = "publish"
	Hold    Action = "pending"
	Drop    Action = "drop"
)

type Decision struct {
	Action Action
	Reason string
}

// Decide applies the mode to one automatically-captured candidate. inOrg is
// whether the session's repo belongs to the team.
func (p Policy) Decide(mode Mode, c Candidate, inOrg bool) Decision {
	if mode == Manual {
		return Decision{Drop, "capture mode is manual"}
	}
	switch c.Sensitivity {
	case "personal":
		return Decision{Drop, "classified as personal"}
	case "sensitive":
		return Decision{Drop, "classified as sensitive"}
	}
	hold := func(reason string) Decision { return Decision{Hold, reason} }
	if !inOrg {
		switch p.OutsideOrg {
		case "skip":
			return Decision{Drop, "session was outside the team's GitHub org"}
		case "review":
			return hold("session was outside the team's GitHub org")
		}
	}
	switch mode {
	case Review, ReviewTimeout:
		return hold("capture mode is " + string(mode))
	case Auto:
		return Decision{Publish, "capture mode is auto"}
	}
	// AI mode.
	if c.Confidence < p.MinConfidence {
		return hold(fmt.Sprintf("low confidence (%.2f)", c.Confidence))
	}
	if c.Contradicts {
		return hold("changes or contradicts an existing memory")
	}
	if c.Redacted {
		return hold("secrets were redacted — check it reads correctly")
	}
	for _, t := range p.AlwaysReviewTypes {
		if strings.EqualFold(t, c.Type) {
			return hold("team reviews all " + c.Type + " memories")
		}
	}
	return Decision{Publish, "confident and shareable"}
}

// InOrg reports whether a repo owner belongs to the team.
func (p Policy) InOrg(memoryOwner, repoOwner string) bool {
	if repoOwner == "" {
		return false
	}
	if strings.EqualFold(repoOwner, memoryOwner) {
		return true
	}
	for _, o := range p.Orgs {
		if strings.EqualFold(o, repoOwner) {
			return true
		}
	}
	return false
}
