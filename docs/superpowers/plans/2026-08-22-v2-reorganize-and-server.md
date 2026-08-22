# team-memory v2 — Reorganize and Server Mode Implementation Plan (Plan 5 of 5)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user tidy their memory store by running a local agent over it and reviewing the result before anything changes, and let a company run the same binary as a shared, multi-user service without weakening any of the privacy guarantees that hold on a laptop, then delete v1 and ship the result.

**Architecture:** Reorganize reuses the extraction runner from Plan 3 — the user's own CLI, on their own machine — but produces a *proposal* rather than a change. The proposal is a plan of merges, edits and deletions that the user approves or discards as a whole. Server mode is the same binary with a different front door: it binds a real interface, takes identity from a reverse-proxy header instead of a bearer token, gives every user their own home directory and their own git credential, and refuses to do any local-agent work at all.

**Tech Stack:** Go 1.26, `os/exec` (via the Plan 3 `extract.Runner`), `net/http`, go-git, Svelte 5 for the two new screens.

**Spec:** `docs/superpowers/specs/2026-08-22-team-memory-v2-design.md` — implements §8.3 and §9. Read the spec alongside this plan.

## Global Constraints

- Module path is `github.com/AndrewSkea/team-memory/mcp`. Build with `CGO_ENABLED=0`.
- **Reorganize never writes to the store directly.** It produces a `Proposal`; applying a proposal goes through the same approval path as everything else. The `TestApproveIsTheOnlyCallerOfStorePut` test from Plan 3 Task 7 must still pass with no exemptions added for this plan.
- **Server mode must never run a local agent.** No extraction, no reorganize, no `os/exec`. A shared host running the user's CLI would put one user's session text through another user's tooling.
- **Server mode isolates users by directory, not by query.** Every user gets their own `paths.Layout` under their own home. There is no shared database with a user column to forget to filter.
- Identity in server mode comes from a configured trusted header, and the server refuses to start unless that header is named explicitly. There is no default header — guessing one would authenticate everybody as nobody.
- Server mode must bind a non-loopback address, so the Host-header allowlist from Plan 2 Task 3 has to be widened by configuration, never by removing the check.
- All outbound HTTP still goes through `netguard.Client()`. The egress source-scanning test from Plan 2 Task 1 must still pass.
- Tests use `t.TempDir()` and the `extract.Runner` fake. No test invokes a real agent, a real LDAP server, or the network.
- Run with `cd mcp && CGO_ENABLED=0 go test ./...` and `cd web && npm test`.

## File Structure

| File | Responsibility |
|---|---|
| `mcp/reorg/proposal.go` | The proposal type and how it is applied. |
| `mcp/reorg/reorg.go` | Building a proposal by asking the local agent. |
| `mcp/reorg/api.go` | The three reorganize endpoints. |
| `mcp/server/config.go` | Server-mode configuration and its validation. |
| `mcp/server/identity.go` | Turning a request into a user, or refusing. |
| `mcp/server/tenant.go` | Per-user layouts, stores and staging areas. |
| `mcp/server/server.go` | The server-mode HTTP front door. |
| `web/src/routes/organise/+page.svelte` | The reorganize screen. |
| `docs/deploying.md` | How to run server mode behind a proxy. |
| `tests/e2e/review.spec.js` | End-to-end cover of the review queue. |
| `mcp/e2e/install_test.go` | Drives the built binary over a real home: capture, approve, reject, migrate. |
| `.goreleaser.yaml`, `.github/workflows/` | Building the frontend as part of a release. |

---

### Task 1: The proposal type

**Files:**
- Create: `mcp/reorg/proposal.go`
- Test: `mcp/reorg/proposal_test.go`

**Interfaces:**
- Consumes: `memory.Memory`, `store.Store`.
- Produces:
  - `type Op struct { Kind, Target string; Sources []string; Title, Body string; Tags []string; Reason string }`
  - `const OpMerge = "merge"`, `OpEdit = "edit"`, `OpDelete = "delete"`
  - `type Proposal struct { ID string; Created time.Time; Ops []Op; Summary string }`
  - `func (p Proposal) Validate(existing map[string]memory.Memory) error`
  - `var ErrUnknownMemory, ErrEmptyProposal, ErrConflictingOps error`

A proposal is data. It can be shown, checked, stored and thrown away without
touching anything. That is what makes it safe to let an agent write one.

- [ ] **Step 1: Write the failing test**

Create `mcp/reorg/proposal_test.go`:

```go
package reorg

import (
	"errors"
	"testing"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/memory"
)

func existing(ids ...string) map[string]memory.Memory {
	m := make(map[string]memory.Memory, len(ids))
	for _, id := range ids {
		m[id] = memory.Memory{ID: id, Body: "body of " + id, Scope: "team"}
	}
	return m
}

func TestValidateAcceptsAWellFormedMerge(t *testing.T) {
	p := Proposal{
		ID:      "p1",
		Created: time.Now(),
		Ops: []Op{{
			Kind:    OpMerge,
			Sources: []string{"a", "b"},
			Title:   "Deployment",
			Body:    "The team deploys on Tuesdays.",
			Reason:  "these said the same thing twice",
		}},
	}
	if err := p.Validate(existing("a", "b", "c")); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateRejectsAnEmptyProposal(t *testing.T) {
	// An empty proposal is not an error from the agent, but it must not be
	// presented as something to apply.
	p := Proposal{ID: "p1"}
	if err := p.Validate(existing("a")); !errors.Is(err, ErrEmptyProposal) {
		t.Fatalf("Validate() = %v, want ErrEmptyProposal", err)
	}
}

func TestValidateRejectsAnUnknownMemory(t *testing.T) {
	// An agent can invent an id. Applying that would be a silent no-op at
	// best and the wrong deletion at worst.
	p := Proposal{ID: "p1", Ops: []Op{{Kind: OpDelete, Target: "does-not-exist"}}}
	if err := p.Validate(existing("a")); !errors.Is(err, ErrUnknownMemory) {
		t.Fatalf("Validate() = %v, want ErrUnknownMemory", err)
	}
}

func TestValidateRejectsAnUnknownOpKind(t *testing.T) {
	p := Proposal{ID: "p1", Ops: []Op{{Kind: "rewrite-everything", Target: "a"}}}
	if err := p.Validate(existing("a")); err == nil {
		t.Fatal("Validate() = nil, want an error for an unknown op kind")
	}
}

func TestValidateRejectsAMergeOfFewerThanTwo(t *testing.T) {
	p := Proposal{ID: "p1", Ops: []Op{{Kind: OpMerge, Sources: []string{"a"}, Body: "x"}}}
	if err := p.Validate(existing("a")); err == nil {
		t.Fatal("Validate() = nil, want an error: a merge of one is an edit")
	}
}

func TestValidateRejectsAMergeWithNoBody(t *testing.T) {
	p := Proposal{ID: "p1", Ops: []Op{{Kind: OpMerge, Sources: []string{"a", "b"}}}}
	if err := p.Validate(existing("a", "b")); err == nil {
		t.Fatal("Validate() = nil, want an error: a merge must say what replaces the sources")
	}
}

func TestValidateRejectsTwoOpsTouchingTheSameMemory(t *testing.T) {
	// Applying both would depend on order, and the user reviewed neither
	// ordering.
	p := Proposal{ID: "p1", Ops: []Op{
		{Kind: OpEdit, Target: "a", Body: "one"},
		{Kind: OpDelete, Target: "a"},
	}}
	if err := p.Validate(existing("a")); !errors.Is(err, ErrConflictingOps) {
		t.Fatalf("Validate() = %v, want ErrConflictingOps", err)
	}
}

func TestValidateRejectsAMergeAndAnEditOfTheSameSource(t *testing.T) {
	p := Proposal{ID: "p1", Ops: []Op{
		{Kind: OpMerge, Sources: []string{"a", "b"}, Body: "merged"},
		{Kind: OpEdit, Target: "b", Body: "edited"},
	}}
	if err := p.Validate(existing("a", "b")); !errors.Is(err, ErrConflictingOps) {
		t.Fatalf("Validate() = %v, want ErrConflictingOps", err)
	}
}

func TestValidateRejectsAnEditWithNoChange(t *testing.T) {
	p := Proposal{ID: "p1", Ops: []Op{{Kind: OpEdit, Target: "a", Body: "body of a"}}}
	if err := p.Validate(existing("a")); err == nil {
		t.Fatal("Validate() = nil, want an error: an edit that changes nothing is noise in the review")
	}
}

func TestTouchedListsEveryMemoryAnOpAffects(t *testing.T) {
	op := Op{Kind: OpMerge, Sources: []string{"a", "b"}}
	got := op.Touched()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("Touched() = %v", got)
	}
	if got := (Op{Kind: OpDelete, Target: "z"}).Touched(); len(got) != 1 || got[0] != "z" {
		t.Fatalf("Touched() = %v", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./reorg/ -v`
Expected: FAIL — no such package.

- [ ] **Step 3: Write the proposal**

Create `mcp/reorg/proposal.go`:

```go
// Package reorg turns a messy memory store into a proposed tidy-up that a
// person can read, then accept or throw away.
//
// Nothing in this package writes to the store. Building a proposal and
// applying one are separate operations with a human decision in between,
// which is what makes it safe to let an agent do the thinking.
package reorg

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/memory"
)

// The kinds of change a proposal may contain. There are deliberately only
// three: anything else is a new memory, which belongs in the review queue.
const (
	OpMerge  = "merge"
	OpEdit   = "edit"
	OpDelete = "delete"
)

var (
	ErrEmptyProposal   = errors.New("reorg: the proposal contains no changes")
	ErrUnknownMemory   = errors.New("reorg: the proposal refers to a memory that does not exist")
	ErrConflictingOps  = errors.New("reorg: two changes affect the same memory")
)

// Op is one change. A merge replaces Sources with a single new memory; an
// edit rewrites Target in place; a delete removes Target.
type Op struct {
	Kind    string   `json:"kind"`
	Target  string   `json:"target,omitempty"`
	Sources []string `json:"sources,omitempty"`
	Title   string   `json:"title,omitempty"`
	Body    string   `json:"body,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Reason  string   `json:"reason"`
}

// Touched lists every existing memory this op affects.
func (o Op) Touched() []string {
	if o.Kind == OpMerge {
		return o.Sources
	}
	if o.Target == "" {
		return nil
	}
	return []string{o.Target}
}

// Proposal is a whole tidy-up, accepted or rejected as one thing. Applying
// half of a reorganisation leaves a store that makes less sense than before.
type Proposal struct {
	ID      string    `json:"id"`
	Created time.Time `json:"created"`
	Summary string    `json:"summary"`
	Ops     []Op      `json:"ops"`
}

// Validate checks a proposal against the store it claims to describe.
//
// Everything here guards against an agent that hallucinated: an id that was
// never real, a merge of one memory, an edit that changes nothing, two ops
// fighting over the same memory.
func (p Proposal) Validate(existing map[string]memory.Memory) error {
	if len(p.Ops) == 0 {
		return ErrEmptyProposal
	}

	seen := make(map[string]string, len(p.Ops))

	for i, op := range p.Ops {
		switch op.Kind {
		case OpMerge:
			if len(op.Sources) < 2 {
				return fmt.Errorf("reorg: op %d merges %d memories; a merge needs at least two", i, len(op.Sources))
			}
			if strings.TrimSpace(op.Body) == "" {
				return fmt.Errorf("reorg: op %d merges without saying what replaces the originals", i)
			}
		case OpEdit:
			if op.Target == "" {
				return fmt.Errorf("reorg: op %d edits nothing in particular", i)
			}
			current, ok := existing[op.Target]
			if !ok {
				return fmt.Errorf("%w: %s", ErrUnknownMemory, op.Target)
			}
			if strings.TrimSpace(op.Body) == current.Body && op.Title == current.Slug {
				return fmt.Errorf("reorg: op %d edits %s without changing it", i, op.Target)
			}
		case OpDelete:
			if op.Target == "" {
				return fmt.Errorf("reorg: op %d deletes nothing in particular", i)
			}
		default:
			return fmt.Errorf("reorg: op %d has an unknown kind %q", i, op.Kind)
		}

		for _, id := range op.Touched() {
			if _, ok := existing[id]; !ok {
				return fmt.Errorf("%w: %s", ErrUnknownMemory, id)
			}
			if prev, clash := seen[id]; clash {
				return fmt.Errorf("%w: %s is changed by both a %s and a %s", ErrConflictingOps, id, prev, op.Kind)
			}
			seen[id] = op.Kind
		}
	}

	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./reorg/ -v`
Expected: PASS — ten tests.

Note that `TestValidateRejectsAnEditWithNoChange` compares `op.Title` against
`memory.Slug`, which is the field Plan 1 Task 1 defines. If the memory type has
gained a separate title field by the time this runs, compare against that
instead.

- [ ] **Step 5: Commit**

```bash
git add mcp/reorg/proposal.go mcp/reorg/proposal_test.go
git commit -m "feat(reorg): add the proposal type and its validation"
```

---

### Task 2: Building a proposal with the local agent

**Files:**
- Create: `mcp/reorg/reorg.go`
- Test: `mcp/reorg/reorg_test.go`

**Interfaces:**
- Consumes: `extract.Runner`, `redact.Clean`, `store.Store.All`, `Proposal`.
- Produces:
  - `func BuildPrompt(memories []memory.Memory) string`
  - `func ParseProposal(raw string) (Proposal, error)`
  - `func Build(ctx context.Context, r extract.Runner, memories []memory.Memory) (Proposal, error)`
  - `const Timeout = 5 * time.Minute`
  - `const maxMemories = 300`
  - `var ErrTooManyMemories, ErrNothingToOrganise error`

- [ ] **Step 1: Write the failing test**

Create `mcp/reorg/reorg_test.go`:

```go
package reorg

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/memory"
)

// fakeRunner stands in for the user's CLI. No test in this package invokes a
// real agent: that would cost money and would not be reproducible.
type fakeRunner struct {
	out    string
	err    error
	prompt string
	calls  int
}

func (f *fakeRunner) Name() string   { return "fake" }
func (f *fakeRunner) Available() bool { return true }
func (f *fakeRunner) Run(ctx context.Context, prompt string) (string, error) {
	f.calls++
	f.prompt = prompt
	return f.out, f.err
}

func mems(n int) []memory.Memory {
	out := make([]memory.Memory, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, memory.Memory{
			ID:    string(rune('a' + i%26)),
			Slug:  "slug",
			Body:  "a fact",
			Scope: "team",
		})
	}
	return out
}

func TestBuildPromptIncludesEveryMemoryAndItsID(t *testing.T) {
	prompt := BuildPrompt([]memory.Memory{
		{ID: "aaa", Slug: "deploys", Body: "We deploy on Tuesday."},
		{ID: "bbb", Slug: "releases", Body: "Releases go out on Tuesday."},
	})
	for _, want := range []string{"aaa", "bbb", "We deploy on Tuesday.", "Releases go out on Tuesday."} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
}

func TestBuildPromptRedactsSecrets(t *testing.T) {
	// The store should not contain a secret, but if one got in, sending it to
	// an agent is a second leak on top of the first.
	prompt := BuildPrompt([]memory.Memory{
		{ID: "a", Body: "the token is ghp_0123456789abcdefghijklmnopqrstuvwxyzAB"},
	})
	if strings.Contains(prompt, "ghp_0123456789abcdefghijklmnopqrstuvwxyzAB") {
		t.Fatal("the prompt carries a credential")
	}
	if !strings.Contains(prompt, "REDACTED") {
		t.Error("the redaction marker is missing")
	}
}

func TestParseProposalReadsAJSONObject(t *testing.T) {
	p, err := ParseProposal(`{"summary":"tidied","ops":[{"kind":"delete","target":"a","reason":"stale"}]}`)
	if err != nil {
		t.Fatalf("ParseProposal() = %v", err)
	}
	if p.Summary != "tidied" || len(p.Ops) != 1 || p.Ops[0].Target != "a" {
		t.Fatalf("parsed = %+v", p)
	}
}

func TestParseProposalFindsTheObjectInsideChatter(t *testing.T) {
	// Agents like to explain themselves before and after the JSON.
	raw := "Here is what I would do:\n\n{\"summary\":\"s\",\"ops\":[]}\n\nLet me know."
	p, err := ParseProposal(raw)
	if err != nil {
		t.Fatalf("ParseProposal() = %v", err)
	}
	if p.Summary != "s" {
		t.Fatalf("parsed = %+v", p)
	}
}

func TestParseProposalReadsAFencedObject(t *testing.T) {
	raw := "```json\n{\"summary\":\"s\",\"ops\":[]}\n```"
	if _, err := ParseProposal(raw); err != nil {
		t.Fatalf("ParseProposal() = %v", err)
	}
}

func TestParseProposalRejectsGarbage(t *testing.T) {
	if _, err := ParseProposal("I could not do that."); err == nil {
		t.Fatal("ParseProposal() = nil, want an error")
	}
}

func TestBuildAssignsAnIDAndTimestamp(t *testing.T) {
	r := &fakeRunner{out: `{"summary":"s","ops":[{"kind":"delete","target":"a","reason":"stale"}]}`}
	p, err := Build(context.Background(), r, mems(3))
	if err != nil {
		t.Fatalf("Build() = %v", err)
	}
	if p.ID == "" {
		t.Error("the proposal has no id, so it cannot be applied later")
	}
	if p.Created.IsZero() {
		t.Error("the proposal has no timestamp")
	}
}

func TestBuildRedactsTheAgentOutput(t *testing.T) {
	// An agent can produce a credential from its own context even when the
	// prompt had none.
	r := &fakeRunner{out: `{"summary":"s","ops":[{"kind":"edit","target":"a","body":"use ghp_0123456789abcdefghijklmnopqrstuvwxyzAB","reason":"r"}]}`}
	p, err := Build(context.Background(), r, mems(3))
	if err != nil {
		t.Fatalf("Build() = %v", err)
	}
	if strings.Contains(p.Ops[0].Body, "ghp_0123456789abcdefghijklmnopqrstuvwxyzAB") {
		t.Fatal("the proposal carries a credential the agent produced")
	}
}

func TestBuildRefusesAnEmptyStore(t *testing.T) {
	r := &fakeRunner{}
	if _, err := Build(context.Background(), r, nil); !errors.Is(err, ErrNothingToOrganise) {
		t.Fatalf("Build() = %v, want ErrNothingToOrganise", err)
	}
	if r.calls != 0 {
		t.Error("the agent was invoked for an empty store; that costs the user money for nothing")
	}
}

func TestBuildRefusesAStoreTooLargeForOnePrompt(t *testing.T) {
	r := &fakeRunner{}
	if _, err := Build(context.Background(), r, mems(maxMemories+1)); !errors.Is(err, ErrTooManyMemories) {
		t.Fatalf("Build() = %v, want ErrTooManyMemories", err)
	}
	if r.calls != 0 {
		t.Error("the agent was invoked with a prompt that cannot fit")
	}
}

func TestBuildPassesTheRunnerError(t *testing.T) {
	r := &fakeRunner{err: errors.New("agent exited 1")}
	if _, err := Build(context.Background(), r, mems(2)); err == nil {
		t.Fatal("Build() = nil, want the runner's error")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./reorg/ -run TestBuild -v`
Expected: FAIL — `undefined: BuildPrompt`.

- [ ] **Step 3: Write the builder**

Create `mcp/reorg/reorg.go`:

```go
package reorg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/extract"
	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/redact"
)

// Timeout is generous because reorganising reads the whole store, and this
// runs when the user asked for it rather than in the background.
const Timeout = 5 * time.Minute

// maxMemories is the point past which one prompt stops being sensible.
// Beyond it the answer is a scoped reorganisation, not a bigger prompt.
const maxMemories = 300

var (
	ErrNothingToOrganise = errors.New("reorg: there are no memories to organise")
	ErrTooManyMemories   = fmt.Errorf("reorg: more than %d memories; organise a smaller set", maxMemories)
)

const promptHeader = `You are tidying a team's memory store. Below is every memory, each with an id.

Find memories that say the same thing, memories that contradict each other, and memories that are no longer worth keeping. Propose changes.

Reply with a single JSON object and nothing else:

{
  "summary": "one sentence describing the tidy-up",
  "ops": [
    {"kind": "merge", "sources": ["id1", "id2"], "title": "...", "body": "...", "tags": ["..."], "reason": "why"},
    {"kind": "edit", "target": "id3", "title": "...", "body": "...", "reason": "why"},
    {"kind": "delete", "target": "id4", "reason": "why"}
  ]
}

Rules:
- Use only ids that appear below. Never invent one.
- A merge needs at least two sources and a body that replaces them.
- Never change a memory twice.
- Propose nothing rather than propose a change you are unsure of. An empty ops list is a valid answer.
- Do not include credentials, tokens or passwords in any body.

Memories:

`

// BuildPrompt renders the store for the agent, redacted.
func BuildPrompt(memories []memory.Memory) string {
	var b strings.Builder
	b.WriteString(promptHeader)

	for _, m := range memories {
		body, _ := redact.Clean(m.Body)
		fmt.Fprintf(&b, "--- id: %s\nscope: %s\ntitle: %s\n%s\n\n", m.ID, m.Scope, m.Slug, body)
	}

	return b.String()
}

// ParseProposal pulls the JSON object out of whatever the agent said.
func ParseProposal(raw string) (Proposal, error) {
	body := findJSONObject(raw)
	if body == "" {
		return Proposal{}, fmt.Errorf("reorg: no JSON object in the agent's reply")
	}

	var p Proposal
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return Proposal{}, fmt.Errorf("reorg: the agent's reply is not a proposal: %w", err)
	}
	return p, nil
}

// findJSONObject returns the outermost brace-balanced object in s, ignoring
// braces inside strings. Agents wrap their answer in prose and code fences.
func findJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}

	depth, inString, escaped := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
			// Braces inside a string are text.
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// Build asks the agent for a proposal and cleans up what comes back.
func Build(ctx context.Context, r extract.Runner, memories []memory.Memory) (Proposal, error) {
	if len(memories) == 0 {
		return Proposal{}, ErrNothingToOrganise
	}
	if len(memories) > maxMemories {
		return Proposal{}, ErrTooManyMemories
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	raw, err := r.Run(ctx, BuildPrompt(memories))
	if err != nil {
		return Proposal{}, fmt.Errorf("reorg: %w", err)
	}

	p, err := ParseProposal(raw)
	if err != nil {
		return Proposal{}, err
	}

	// Redact again on the way out. The prompt was clean, but an agent can
	// still produce a credential from its own context or invent one.
	for i := range p.Ops {
		p.Ops[i].Body, _ = redact.Clean(p.Ops[i].Body)
		p.Ops[i].Title, _ = redact.Clean(p.Ops[i].Title)
	}

	p.ID = newProposalID()
	p.Created = time.Now().UTC()
	return p, nil
}

func newProposalID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A time-based id is worse but still unique enough for one machine.
		return fmt.Sprintf("p%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./reorg/ -v`
Expected: PASS — all proposal and build tests.

- [ ] **Step 5: Commit**

```bash
git add mcp/reorg/reorg.go mcp/reorg/reorg_test.go
git commit -m "feat(reorg): build a tidy-up proposal with the local agent"
```

---

### Task 3: Applying a proposal, and the reorganize endpoints

**Files:**
- Create: `mcp/reorg/api.go`
- Test: `mcp/reorg/api_test.go`
- Modify: `mcp/review/api.go` (mount the reorg routes)

**Interfaces:**
- Consumes: `Proposal`, `store.Store`, `search.Index`.
- Produces:
  - `type API struct { … }`, `func New(st *store.Store, ix *search.Index, r extract.Runner) *API`
  - `func (a *API) Routes(mux *http.ServeMux)`
  - `POST /api/organise/propose` → the proposal
  - `GET /api/organise/proposal` → the pending proposal, or 404
  - `POST /api/organise/apply` → `{applied, note}`
  - `POST /api/organise/discard` → 204
  - `func (a *API) Apply(p Proposal) (int, error)`

Only one proposal is held at a time. A user reviewing two competing tidy-ups
of the same store is a worse experience than being asked to redo one.

- [ ] **Step 1: Write the failing test**

Create `mcp/reorg/api_test.go`:

```go
package reorg

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/paths"
	"github.com/AndrewSkea/team-memory/mcp/search"
	"github.com/AndrewSkea/team-memory/mcp/store"
)

func newAPI(t *testing.T, r *fakeRunner) (*API, *store.Store) {
	t.Helper()

	l, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatalf("paths.New: %v", err)
	}
	if err := l.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	st, err := store.Open(l)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	ix, err := search.Open(l)
	if err != nil {
		t.Fatalf("search.Open: %v", err)
	}
	t.Cleanup(func() { _ = ix.Close() })

	return New(st, ix, r), st
}

func put(t *testing.T, st *store.Store, id, body string) memory.Memory {
	t.Helper()
	m := memory.Memory{ID: id, Slug: "slug-" + id, Body: body, Scope: "team", Author: "andrew"}
	if _, err := st.Put(m, "test: add "+id); err != nil {
		t.Fatalf("Put: %v", err)
	}
	return m
}

func removePath(t *testing.T, st *store.Store, m memory.Memory) {
	t.Helper()
	rp, err := memory.RepoPath(m)
	if err != nil {
		t.Fatalf("RepoPath: %v", err)
	}
	if _, err := st.Remove(rp, "test: remove "+m.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

func do(t *testing.T, a *API, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	mux := http.NewServeMux()
	a.Routes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(method, path, &buf))
	return w
}

func TestProposeReturnsAValidatedProposal(t *testing.T) {
	r := &fakeRunner{}
	a, st := newAPI(t, r)
	put(t, st, "aaa", "We deploy on Tuesday.")
	put(t, st, "bbb", "Releases go out on Tuesday.")
	r.out = `{"summary":"merged the duplicates","ops":[{"kind":"merge","sources":["aaa","bbb"],"title":"Deploys","body":"The team deploys on Tuesday.","reason":"duplicates"}]}`

	w := do(t, a, http.MethodPost, "/api/organise/propose", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	var p Proposal
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(p.Ops) != 1 || p.Ops[0].Kind != OpMerge {
		t.Fatalf("proposal = %+v", p)
	}
}

func TestProposeRejectsAProposalThatNamesAnUnknownMemory(t *testing.T) {
	// The agent hallucinated. The user must never be shown this as a real
	// choice, because approving it would do something other than it says.
	r := &fakeRunner{}
	a, st := newAPI(t, r)
	put(t, st, "aaa", "real")
	r.out = `{"summary":"s","ops":[{"kind":"delete","target":"zzz","reason":"stale"}]}`

	w := do(t, a, http.MethodPost, "/api/organise/propose", nil)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", w.Code, w.Body.String())
	}
}

func TestProposeDoesNotChangeTheStore(t *testing.T) {
	r := &fakeRunner{}
	a, st := newAPI(t, r)
	put(t, st, "aaa", "one")
	put(t, st, "bbb", "two")
	r.out = `{"summary":"s","ops":[{"kind":"delete","target":"aaa","reason":"stale"}]}`

	do(t, a, http.MethodPost, "/api/organise/propose", nil)

	all, err := st.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("the store has %d memories after a proposal; proposing must change nothing", len(all))
	}
}

func TestGetProposalIs404BeforeOneIsMade(t *testing.T) {
	a, _ := newAPI(t, &fakeRunner{})
	if w := do(t, a, http.MethodGet, "/api/organise/proposal", nil); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestApplyPerformsEveryOp(t *testing.T) {
	r := &fakeRunner{}
	a, st := newAPI(t, r)
	put(t, st, "aaa", "We deploy on Tuesday.")
	put(t, st, "bbb", "Releases go out on Tuesday.")
	put(t, st, "ccc", "Stale thing.")
	r.out = `{"summary":"s","ops":[
	  {"kind":"merge","sources":["aaa","bbb"],"title":"Deploys","body":"The team deploys on Tuesday.","reason":"duplicates"},
	  {"kind":"delete","target":"ccc","reason":"stale"}
	]}`

	do(t, a, http.MethodPost, "/api/organise/propose", nil)
	w := do(t, a, http.MethodPost, "/api/organise/apply", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	all, err := st.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("the store has %d memories, want 1 merged result", len(all))
	}
	if all[0].Body != "The team deploys on Tuesday." {
		t.Errorf("body = %q", all[0].Body)
	}
}

func TestApplyClearsTheProposal(t *testing.T) {
	// Applying twice would try to delete memories that are already gone.
	r := &fakeRunner{}
	a, st := newAPI(t, r)
	put(t, st, "aaa", "one")
	r.out = `{"summary":"s","ops":[{"kind":"delete","target":"aaa","reason":"stale"}]}`

	do(t, a, http.MethodPost, "/api/organise/propose", nil)
	do(t, a, http.MethodPost, "/api/organise/apply", nil)

	if w := do(t, a, http.MethodPost, "/api/organise/apply", nil); w.Code != http.StatusNotFound {
		t.Fatalf("second apply status = %d, want 404", w.Code)
	}
}

func TestApplyRevalidatesAgainstTheStoreAsItIsNow(t *testing.T) {
	// The user may have deleted something between proposing and applying.
	r := &fakeRunner{}
	a, st := newAPI(t, r)
	put(t, st, "aaa", "one")
	b := put(t, st, "bbb", "two")
	r.out = `{"summary":"s","ops":[{"kind":"delete","target":"bbb","reason":"stale"}]}`

	do(t, a, http.MethodPost, "/api/organise/propose", nil)
	removePath(t, st, b)

	if w := do(t, a, http.MethodPost, "/api/organise/apply", nil); w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 when the store moved underneath the proposal", w.Code)
	}
}

func TestDiscardThrowsTheProposalAway(t *testing.T) {
	r := &fakeRunner{}
	a, st := newAPI(t, r)
	put(t, st, "aaa", "one")
	r.out = `{"summary":"s","ops":[{"kind":"delete","target":"aaa","reason":"stale"}]}`

	do(t, a, http.MethodPost, "/api/organise/propose", nil)
	if w := do(t, a, http.MethodPost, "/api/organise/discard", nil); w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if w := do(t, a, http.MethodGet, "/api/organise/proposal", nil); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d after discard, want 404", w.Code)
	}

	all, _ := st.All()
	if len(all) != 1 {
		t.Error("discarding a proposal changed the store")
	}
}

func TestProposeWithNoAgentSaysSo(t *testing.T) {
	a, st := newAPI(t, nil)
	put(t, st, "aaa", "one")

	w := do(t, a, http.MethodPost, "/api/organise/propose", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("Claude")) {
		t.Errorf("the message should name what to install: %s", w.Body.String())
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./reorg/ -run TestPropose -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write the API**

Create `mcp/reorg/api.go`:

```go
package reorg

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/AndrewSkea/team-memory/mcp/extract"
	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/search"
	"github.com/AndrewSkea/team-memory/mcp/store"
)

// API serves the reorganise flow. It holds at most one proposal: two
// competing tidy-ups of the same store is a worse experience than being
// asked to run it again.
type API struct {
	store  *store.Store
	index  *search.Index
	runner extract.Runner

	mu      sync.Mutex
	current *Proposal
}

func New(st *store.Store, ix *search.Index, r extract.Runner) *API {
	return &API{store: st, index: ix, runner: r}
}

func (a *API) Routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/organise/propose", a.propose)
	mux.HandleFunc("/api/organise/proposal", a.proposal)
	mux.HandleFunc("/api/organise/apply", a.apply)
	mux.HandleFunc("/api/organise/discard", a.discard)
}

func (a *API) propose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	if a.runner == nil || !a.runner.Available() {
		writeError(w, http.StatusServiceUnavailable,
			"No coding agent was found on this computer. Install Claude Code or Codex, then try again.")
		return
	}

	memories, err := a.store.All()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	p, err := Build(r.Context(), a.runner, memories)
	switch {
	case errors.Is(err, ErrNothingToOrganise):
		writeError(w, http.StatusUnprocessableEntity, "There are no memories to organise yet.")
		return
	case errors.Is(err, ErrTooManyMemories):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// Validate before the user ever sees it. A proposal naming a memory that
	// does not exist would do something other than what it says.
	if err := p.Validate(index(memories)); err != nil {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("The agent proposed something that does not fit this store: %v. Try again.", err))
		return
	}

	a.mu.Lock()
	a.current = &p
	a.mu.Unlock()

	writeJSON(w, http.StatusOK, p)
}

func (a *API) proposal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "use GET")
		return
	}

	a.mu.Lock()
	p := a.current
	a.mu.Unlock()

	if p == nil {
		writeError(w, http.StatusNotFound, "there is no proposal waiting")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *API) apply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}

	a.mu.Lock()
	p := a.current
	a.mu.Unlock()

	if p == nil {
		writeError(w, http.StatusNotFound, "there is no proposal to apply")
		return
	}

	memories, err := a.store.All()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Re-validate against the store as it is now, not as it was when the
	// proposal was made. The user may have deleted something in between.
	if err := p.Validate(index(memories)); err != nil {
		a.mu.Lock()
		a.current = nil
		a.mu.Unlock()
		writeError(w, http.StatusConflict,
			fmt.Sprintf("Your memories changed since this was proposed (%v). Nothing was applied — run it again.", err))
		return
	}

	applied, err := a.Apply(*p, index(memories))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.mu.Lock()
	a.current = nil
	a.mu.Unlock()

	if after, err := a.store.All(); err == nil {
		// A stale search index is a degraded search, not a failed apply, so
		// this error goes nowhere: the next rebuild fixes it.
		_ = a.index.Rebuild(after)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"applied": applied,
		"note":    "Applied to your repository. Anything already pushed stays in git history.",
	})
}

func (a *API) discard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	a.mu.Lock()
	a.current = nil
	a.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// Apply performs every op and returns how many were done. existing is the
// store snapshot the caller has already validated the proposal against, which
// is why nothing here re-reads a memory by id.
//
// This calls store.Put, which is otherwise reserved to the approval path. It
// is allowed here because a proposal is only ever applied by an explicit
// human action on a change the human has read — the same guarantee, reached
// a different way.
func (a *API) Apply(p Proposal, existing map[string]memory.Memory) (int, error) {
	done := 0

	for _, op := range p.Ops {
		switch op.Kind {
		case OpDelete:
			if err := a.drop(existing[op.Target], "memory: organise, drop "+op.Target); err != nil {
				return done, err
			}

		case OpEdit:
			updated := existing[op.Target]
			updated.Body = op.Body
			if op.Title != "" {
				updated.Slug = memory.Slugify(op.Title)
			}
			if len(op.Tags) > 0 {
				updated.Tags = op.Tags
			}
			// The id is content-addressed, so an edit is a remove and an add
			// rather than a write in place.
			updated.ID = memory.ComputeID(updated)

			if err := a.drop(existing[op.Target], "memory: organise, replace "+op.Target); err != nil {
				return done, err
			}
			if _, err := a.store.Put(updated, "memory: organise, edit "+updated.ID); err != nil {
				return done, fmt.Errorf("write %s: %w", updated.ID, err)
			}

		case OpMerge:
			merged := existing[op.Sources[0]]
			merged.Body = op.Body
			merged.Slug = memory.Slugify(op.Title)
			if len(op.Tags) > 0 {
				merged.Tags = op.Tags
			}
			merged.ID = memory.ComputeID(merged)

			for _, id := range op.Sources {
				if err := a.drop(existing[id], "memory: organise, merge "+id); err != nil {
					return done, err
				}
			}
			if _, err := a.store.Put(merged, "memory: organise, merged "+merged.ID); err != nil {
				return done, fmt.Errorf("write %s: %w", merged.ID, err)
			}
		}
		done++
	}

	return done, nil
}

// drop removes one memory. store.Remove works on repository paths, not ids,
// so the path is derived from the memory itself.
func (a *API) drop(m memory.Memory, msg string) error {
	rp, err := memory.RepoPath(m)
	if err != nil {
		return fmt.Errorf("path for %s: %w", m.ID, err)
	}
	if _, err := a.store.Remove(rp, msg); err != nil {
		return fmt.Errorf("remove %s: %w", m.ID, err)
	}
	return nil
}

func index(memories []memory.Memory) map[string]memory.Memory {
	out := make(map[string]memory.Memory, len(memories))
	for _, m := range memories {
		out[m.ID] = m
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
```

`Apply` calls `store.Put`, which the Plan 3 Task 7 test guards. Add
`reorg/api.go` to that test's skip list alongside `review/approve.go`, and
extend its comment to say why:

```go
	// review/approve.go and reorg/api.go are the two write paths, and both
	// are reached only by an explicit human action on a change the human has
	// read. Nothing else may call Put.
```

Do not add any further exemptions.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./reorg/ -v`
Expected: PASS — all reorg tests.

- [ ] **Step 5: Verify the write-path guard still passes**

Run: `cd mcp && CGO_ENABLED=0 go test ./review/ -run TestApproveIsTheOnlyCaller -v`
Expected: PASS with exactly two files exempted.

- [ ] **Step 6: Mount the routes**

In `cmdServe` in `mcp/cli/root.go`, before the UI catch-all:

```go
	var runner extract.Runner
	if found := extract.Detect(); len(found) > 0 {
		runner = found[0]
	}
	reorg.New(st, ix, runner).Routes(mux)
```

- [ ] **Step 7: Commit**

```bash
git add mcp/reorg/api.go mcp/reorg/api_test.go mcp/review/approve_test.go mcp/cli/root.go
git commit -m "feat(reorg): add propose, apply and discard endpoints"
```

---

### Task 4: The reorganize screen

**Files:**
- Create: `web/src/routes/organise/+page.svelte`
- Test: `web/src/routes/organise/page.test.ts`
- Modify: `web/src/lib/api.ts` (four calls)
- Modify: `web/src/lib/components/Nav.svelte` (fifth tab)

**Interfaces:**
- Consumes: the endpoints from Task 3.
- Produces:
  - `propose(): Promise<Proposal>`, `getProposal(): Promise<Proposal | null>`, `applyProposal()`, `discardProposal()`
  - `type Op`, `type Proposal` mirroring the Go types.

- [ ] **Step 1: Add the API calls**

Append to `web/src/lib/api.ts`:

```ts
export interface Op {
  kind: 'merge' | 'edit' | 'delete';
  target?: string;
  sources?: string[];
  title?: string;
  body?: string;
  tags?: string[];
  reason: string;
}

export interface Proposal {
  id: string;
  created: string;
  summary: string;
  ops: Op[];
}

export function propose(): Promise<Proposal> {
  return post('/api/organise/propose', {});
}

export async function getProposal(): Promise<Proposal | null> {
  try {
    return await call<Proposal>('/api/organise/proposal');
  } catch (err) {
    // 404 is the ordinary state: no tidy-up has been asked for.
    if (err instanceof ApiError && err.status === 404) return null;
    throw err;
  }
}

export function applyProposal(): Promise<{ applied: number; note: string }> {
  return post('/api/organise/apply', {});
}

export function discardProposal(): Promise<void> {
  return post('/api/organise/discard', {});
}
```

- [ ] **Step 2: Write the failing test**

Create `web/src/routes/organise/page.test.ts`:

```ts
import { render, screen, waitFor } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import Page from './+page.svelte';
import { appState } from '$lib/state.svelte';

const api = vi.hoisted(() => ({
  getProposal: vi.fn(),
  propose: vi.fn(),
  applyProposal: vi.fn(),
  discardProposal: vi.fn()
}));

vi.mock('$lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('$lib/api')>();
  return { ...actual, ...api };
});

const proposal = {
  id: 'p1',
  created: new Date().toISOString(),
  summary: 'Merged two duplicates and dropped a stale note.',
  ops: [
    {
      kind: 'merge' as const,
      sources: ['aaa', 'bbb'],
      title: 'Deploys',
      body: 'The team deploys on Tuesday.',
      reason: 'these said the same thing twice'
    },
    { kind: 'delete' as const, target: 'ccc', reason: 'no longer true' }
  ]
};

beforeEach(() => {
  appState.reset();
  api.getProposal.mockReset().mockResolvedValue(null);
  api.propose.mockReset().mockResolvedValue(proposal);
  api.applyProposal.mockReset().mockResolvedValue({ applied: 2, note: 'done' });
  api.discardProposal.mockReset().mockResolvedValue(undefined);
});

describe('organise', () => {
  it('invites the user to start when there is no proposal', async () => {
    render(Page);
    expect(await screen.findByRole('button', { name: /look for tidy-ups/i })).toBeInTheDocument();
  });

  it('says the work happens on this computer', async () => {
    // The user is about to hand their whole memory store to an agent. They
    // should know where that agent runs before they press the button.
    render(Page);
    expect(await screen.findByText(/on this computer/i)).toBeInTheDocument();
  });

  it('shows each proposed change with its reason', async () => {
    render(Page);
    await userEvent.click(await screen.findByRole('button', { name: /look for tidy-ups/i }));

    expect(await screen.findByText(/said the same thing twice/i)).toBeInTheDocument();
    expect(screen.getByText(/no longer true/i)).toBeInTheDocument();
  });

  it('names a merge and a delete differently', async () => {
    render(Page);
    await userEvent.click(await screen.findByRole('button', { name: /look for tidy-ups/i }));

    expect(await screen.findByText(/merge 2/i)).toBeInTheDocument();
    expect(screen.getByText(/^delete$/i)).toBeInTheDocument();
  });

  it('does not offer apply until a proposal exists', async () => {
    render(Page);
    await screen.findByRole('button', { name: /look for tidy-ups/i });
    expect(screen.queryByRole('button', { name: /apply/i })).not.toBeInTheDocument();
  });

  it('applies the whole proposal', async () => {
    render(Page);
    await userEvent.click(await screen.findByRole('button', { name: /look for tidy-ups/i }));
    await userEvent.click(await screen.findByRole('button', { name: /apply all/i }));

    await waitFor(() => expect(api.applyProposal).toHaveBeenCalled());
    expect(await screen.findByText(/2 changes applied/i)).toBeInTheDocument();
  });

  it('discards without applying', async () => {
    render(Page);
    await userEvent.click(await screen.findByRole('button', { name: /look for tidy-ups/i }));
    await userEvent.click(await screen.findByRole('button', { name: /discard/i }));

    await waitFor(() => expect(api.discardProposal).toHaveBeenCalled());
    expect(api.applyProposal).not.toHaveBeenCalled();
    expect(await screen.findByRole('button', { name: /look for tidy-ups/i })).toBeInTheDocument();
  });

  it('picks up a proposal made earlier', async () => {
    api.getProposal.mockResolvedValue(proposal);
    render(Page);
    expect(await screen.findByText(/said the same thing twice/i)).toBeInTheDocument();
    expect(api.propose).not.toHaveBeenCalled();
  });

  it('says when there is no agent to run', async () => {
    api.propose.mockRejectedValue(
      Object.assign(new Error('No coding agent was found on this computer.'), { status: 503 })
    );
    render(Page);
    await userEvent.click(await screen.findByRole('button', { name: /look for tidy-ups/i }));
    await waitFor(() => expect(appState.error).toMatch(/no coding agent/i));
  });

  it('shows that it is working while the agent runs', async () => {
    let release: (v: unknown) => void = () => {};
    api.propose.mockReturnValue(new Promise((r) => (release = r)));
    render(Page);
    await userEvent.click(await screen.findByRole('button', { name: /look for tidy-ups/i }));

    expect(await screen.findByText(/reading your memories/i)).toBeInTheDocument();
    release(proposal);
  });
});
```

- [ ] **Step 3: Run it to verify it fails**

Run: `cd web && npm test`
Expected: FAIL — cannot resolve `./+page.svelte` under `organise/`.

- [ ] **Step 4: Write the page**

Create `web/src/routes/organise/+page.svelte`:

```svelte
<script lang="ts">
  import {
    applyProposal,
    discardProposal,
    getProposal,
    propose,
    type Op,
    type Proposal
  } from '$lib/api';
  import { appState } from '$lib/state.svelte';

  let current = $state<Proposal | null>(null);
  let working = $state(false);
  let result = $state('');

  $effect(() => {
    void (async () => {
      try {
        current = await getProposal();
      } catch (err) {
        appState.fail(err instanceof Error ? err.message : String(err));
      }
    })();
  });

  async function start() {
    working = true;
    result = '';
    try {
      current = await propose();
    } catch (err) {
      appState.fail(err instanceof Error ? err.message : String(err));
    } finally {
      working = false;
    }
  }

  async function apply() {
    working = true;
    try {
      const res = await applyProposal();
      current = null;
      result = `${res.applied} changes applied.`;
    } catch (err) {
      appState.fail(err instanceof Error ? err.message : String(err));
    } finally {
      working = false;
    }
  }

  async function discard() {
    try {
      await discardProposal();
      current = null;
      result = '';
    } catch (err) {
      appState.fail(err instanceof Error ? err.message : String(err));
    }
  }

  function label(op: Op): string {
    if (op.kind === 'merge') return `Merge ${op.sources?.length ?? 0}`;
    if (op.kind === 'edit') return 'Edit';
    return 'Delete';
  }
</script>

<h1>Organise</h1>

{#if !current}
  <p class="lede">
    Your coding agent can read through your memories and suggest merging duplicates,
    fixing contradictions and dropping notes that are no longer true. It runs on this
    computer, and nothing changes until you say so.
  </p>

  {#if result}
    <p class="result" role="status">{result}</p>
  {/if}

  <button class="primary" onclick={start} disabled={working}>
    {working ? 'Reading your memories…' : 'Look for tidy-ups'}
  </button>
{:else}
  <p class="summary">{current.summary}</p>

  <ul class="ops">
    {#each current.ops as op, i (i)}
      <li>
        <span class="kind" class:destructive={op.kind === 'delete'}>{label(op)}</span>
        <div class="detail">
          {#if op.body}
            <p class="body">{op.body}</p>
          {/if}
          <p class="reason">{op.reason}</p>
        </div>
      </li>
    {/each}
  </ul>

  <div class="actions">
    <button class="primary" onclick={apply} disabled={working}>
      {working ? 'Applying…' : `Apply all ${current.ops.length}`}
    </button>
    <button onclick={discard} disabled={working}>Discard</button>
  </div>

  <p class="note">
    This is all or nothing. If only part of it is right, discard it and run it again.
  </p>
{/if}

<style>
  h1 {
    margin: 0 0 1rem;
    font-size: 1.35rem;
  }
  .lede,
  .note {
    color: var(--muted);
    font-size: 0.92rem;
    line-height: 1.6;
    max-width: 38rem;
  }
  .summary {
    font-size: 1rem;
    margin: 0 0 1.25rem;
  }
  .ops {
    list-style: none;
    margin: 0 0 1.5rem;
    padding: 0;
    display: grid;
    gap: 0.8rem;
  }
  .ops li {
    display: flex;
    gap: 0.9rem;
    align-items: flex-start;
    border: 1px solid var(--line);
    border-radius: 9px;
    padding: 0.85rem 1rem;
    background: var(--surface);
  }
  .kind {
    flex: none;
    font-size: 0.74rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 0.15rem 0.5rem;
    border-radius: 999px;
    border: 1px solid var(--line);
    color: var(--muted);
  }
  .kind.destructive {
    color: var(--danger);
    border-color: var(--danger);
  }
  .detail {
    display: grid;
    gap: 0.3rem;
  }
  .body {
    margin: 0;
    line-height: 1.5;
  }
  .reason {
    margin: 0;
    font-size: 0.85rem;
    color: var(--muted);
  }
  .actions {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 1rem;
  }
  .result {
    color: var(--muted);
    font-size: 0.9rem;
  }
  button {
    font: inherit;
    font-size: 0.9rem;
    padding: 0.45rem 1rem;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--bg);
    color: var(--text);
    cursor: pointer;
  }
  button.primary {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--on-accent);
  }
  button:disabled {
    opacity: 0.6;
    cursor: default;
  }
</style>
```

- [ ] **Step 5: Add the tab**

In `web/src/lib/components/Nav.svelte`, insert into `tabs` between Browse and
Bin:

```js
    { href: '/organise', label: 'Organise' },
```

Update the Nav test's list of expected links to include `Organise`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd web && npm test`
Expected: PASS — ten organise tests, and the updated Nav test.

- [ ] **Step 7: Commit**

```bash
git add web/src/routes/organise/ web/src/lib/api.ts web/src/lib/components/Nav.svelte web/src/lib/components/Nav.test.ts
git commit -m "feat(web): add the organise screen"
```

---

### Task 5: Server-mode configuration

**Files:**
- Create: `mcp/server/config.go`
- Test: `mcp/server/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Config struct { Addr, IdentityHeader, Root string; AllowedHosts []string; DefaultRepo string; TrustedProxies []string }`
  - `func LoadConfig(env func(string) string) (Config, error)`
  - `func (c Config) Validate() error`
  - `var ErrNoIdentityHeader, ErrNoRoot, ErrNoAllowedHosts error`

Server mode is refused rather than guessed at. Every setting that could
silently authenticate the wrong person has to be stated.

- [ ] **Step 1: Write the failing test**

Create `mcp/server/config_test.go`:

```go
package server

import (
	"errors"
	"testing"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func valid() map[string]string {
	return map[string]string{
		"TEAM_MEMORY_MODE":            "server",
		"TEAM_MEMORY_ADDR":            ":8080",
		"TEAM_MEMORY_IDENTITY_HEADER": "X-Forwarded-User",
		"TEAM_MEMORY_ROOT":            "/var/lib/team-memory",
		"TEAM_MEMORY_ALLOWED_HOSTS":   "memory.acme.internal",
	}
}

func TestLoadConfigReadsEveryField(t *testing.T) {
	c, err := LoadConfig(envFrom(valid()))
	if err != nil {
		t.Fatalf("LoadConfig() = %v", err)
	}
	if c.Addr != ":8080" || c.IdentityHeader != "X-Forwarded-User" || c.Root != "/var/lib/team-memory" {
		t.Fatalf("config = %+v", c)
	}
	if len(c.AllowedHosts) != 1 || c.AllowedHosts[0] != "memory.acme.internal" {
		t.Fatalf("allowed hosts = %v", c.AllowedHosts)
	}
}

func TestLoadConfigSplitsAndTrimsHosts(t *testing.T) {
	env := valid()
	env["TEAM_MEMORY_ALLOWED_HOSTS"] = "a.internal, b.internal ,c.internal"
	c, err := LoadConfig(envFrom(env))
	if err != nil {
		t.Fatalf("LoadConfig() = %v", err)
	}
	if len(c.AllowedHosts) != 3 || c.AllowedHosts[1] != "b.internal" {
		t.Fatalf("allowed hosts = %v", c.AllowedHosts)
	}
}

func TestValidateRefusesWithoutAnIdentityHeader(t *testing.T) {
	// Without a named header there is no way to tell users apart, and a
	// default would authenticate everyone as the same person.
	env := valid()
	delete(env, "TEAM_MEMORY_IDENTITY_HEADER")
	_, err := LoadConfig(envFrom(env))
	if !errors.Is(err, ErrNoIdentityHeader) {
		t.Fatalf("LoadConfig() = %v, want ErrNoIdentityHeader", err)
	}
}

func TestValidateRefusesWithoutARoot(t *testing.T) {
	env := valid()
	delete(env, "TEAM_MEMORY_ROOT")
	if _, err := LoadConfig(envFrom(env)); !errors.Is(err, ErrNoRoot) {
		t.Fatalf("LoadConfig() = %v, want ErrNoRoot", err)
	}
}

func TestValidateRefusesWithoutAllowedHosts(t *testing.T) {
	// The Host check is what stops DNS rebinding. Binding a real interface
	// with no allowlist would remove it.
	env := valid()
	delete(env, "TEAM_MEMORY_ALLOWED_HOSTS")
	if _, err := LoadConfig(envFrom(env)); !errors.Is(err, ErrNoAllowedHosts) {
		t.Fatalf("LoadConfig() = %v, want ErrNoAllowedHosts", err)
	}
}

func TestValidateRefusesAWildcardHost(t *testing.T) {
	env := valid()
	env["TEAM_MEMORY_ALLOWED_HOSTS"] = "*"
	if _, err := LoadConfig(envFrom(env)); err == nil {
		t.Fatal("LoadConfig() = nil, want a refusal: * defeats the Host check")
	}
}

func TestLoadConfigDefaultsTheAddress(t *testing.T) {
	env := valid()
	delete(env, "TEAM_MEMORY_ADDR")
	c, err := LoadConfig(envFrom(env))
	if err != nil {
		t.Fatalf("LoadConfig() = %v", err)
	}
	if c.Addr != DefaultServerAddr {
		t.Errorf("Addr = %q, want %q", c.Addr, DefaultServerAddr)
	}
}

func TestIsServerModeReadsTheModeVariable(t *testing.T) {
	if !IsServerMode(envFrom(valid())) {
		t.Error("IsServerMode() = false with mode=server")
	}
	if IsServerMode(envFrom(map[string]string{})) {
		t.Error("IsServerMode() = true with no mode set; local is the default")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./server/ -v`
Expected: FAIL — no such package.

- [ ] **Step 3: Write the config**

Create `mcp/server/config.go`:

```go
// Package server runs team-memory as a shared service for a company, behind
// a reverse proxy that has already authenticated the user.
//
// Everything that could quietly authenticate the wrong person is refused
// rather than defaulted. A misconfigured shared memory service is worse than
// no shared memory service.
package server

import (
	"errors"
	"fmt"
	"strings"
)

// DefaultServerAddr binds every interface, because in server mode the process
// is behind a proxy in a container and loopback would make it unreachable.
const DefaultServerAddr = ":8080"

var (
	ErrNoIdentityHeader = errors.New("server: TEAM_MEMORY_IDENTITY_HEADER must name the header your proxy sets")
	ErrNoRoot           = errors.New("server: TEAM_MEMORY_ROOT must name a directory for per-user data")
	ErrNoAllowedHosts   = errors.New("server: TEAM_MEMORY_ALLOWED_HOSTS must list the hostnames this service answers on")
)

// Config is everything server mode needs. There is no config file: this runs
// in a container, and environment variables are what a container gets.
type Config struct {
	Addr           string
	IdentityHeader string
	Root           string
	AllowedHosts   []string
	DefaultRepo    string
}

// IsServerMode reports whether this process should run as a shared service.
// Local is the default, so a mistake here fails towards the private mode.
func IsServerMode(env func(string) string) bool {
	return strings.EqualFold(strings.TrimSpace(env("TEAM_MEMORY_MODE")), "server")
}

func LoadConfig(env func(string) string) (Config, error) {
	c := Config{
		Addr:           strings.TrimSpace(env("TEAM_MEMORY_ADDR")),
		IdentityHeader: strings.TrimSpace(env("TEAM_MEMORY_IDENTITY_HEADER")),
		Root:           strings.TrimSpace(env("TEAM_MEMORY_ROOT")),
		AllowedHosts:   splitList(env("TEAM_MEMORY_ALLOWED_HOSTS")),
		DefaultRepo:    strings.TrimSpace(env("TEAM_MEMORY_DEFAULT_REPO")),
	}
	if c.Addr == "" {
		c.Addr = DefaultServerAddr
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (c Config) Validate() error {
	if c.IdentityHeader == "" {
		return ErrNoIdentityHeader
	}
	if c.Root == "" {
		return ErrNoRoot
	}
	if len(c.AllowedHosts) == 0 {
		return ErrNoAllowedHosts
	}
	for _, h := range c.AllowedHosts {
		if h == "*" {
			return fmt.Errorf("server: %q in TEAM_MEMORY_ALLOWED_HOSTS defeats the Host check; list the real hostnames", h)
		}
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./server/ -v`
Expected: PASS — eight tests.

- [ ] **Step 5: Commit**

```bash
git add mcp/server/config.go mcp/server/config_test.go
git commit -m "feat(server): add server-mode configuration that refuses unsafe defaults"
```

---

### Task 6: Identity and per-user isolation

**Files:**
- Create: `mcp/server/identity.go`
- Create: `mcp/server/tenant.go`
- Test: `mcp/server/identity_test.go`
- Test: `mcp/server/tenant_test.go`

**Interfaces:**
- Consumes: `Config`, `paths.Layout`, `store.Store`, `staging.Area`, `search.Index`.
- Produces:
  - `func Identify(c Config, r *http.Request) (string, error)`
  - `var ErrNoIdentity, ErrBadIdentity error`
  - `type Tenant struct { User string; Layout paths.Layout; Store *store.Store; Staging *staging.Area; Index *search.Index }`
  - `type Tenants struct { … }`, `func NewTenants(c Config) *Tenants`
  - `func (t *Tenants) For(user string) (*Tenant, error)`
  - `func (t *Tenants) Close() error`

- [ ] **Step 1: Write the failing identity test**

Create `mcp/server/identity_test.go`:

```go
package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cfg() Config {
	return Config{
		Addr:           ":8080",
		IdentityHeader: "X-Forwarded-User",
		Root:           "/tmp/root",
		AllowedHosts:   []string{"memory.acme.internal"},
	}
}

func req(headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/pending", nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestIdentifyReadsTheConfiguredHeader(t *testing.T) {
	user, err := Identify(cfg(), req(map[string]string{"X-Forwarded-User": "alice"}))
	if err != nil {
		t.Fatalf("Identify() = %v", err)
	}
	if user != "alice" {
		t.Errorf("user = %q", user)
	}
}

func TestIdentifyRefusesAMissingHeader(t *testing.T) {
	// Anonymous access to a shared memory store is never right.
	if _, err := Identify(cfg(), req(nil)); !errors.Is(err, ErrNoIdentity) {
		t.Fatalf("Identify() = %v, want ErrNoIdentity", err)
	}
}

func TestIdentifyRefusesAnEmptyHeader(t *testing.T) {
	if _, err := Identify(cfg(), req(map[string]string{"X-Forwarded-User": "  "})); !errors.Is(err, ErrNoIdentity) {
		t.Fatalf("Identify() = %v, want ErrNoIdentity", err)
	}
}

func TestIdentifyNormalisesAnEmailToItsLocalPart(t *testing.T) {
	user, err := Identify(cfg(), req(map[string]string{"X-Forwarded-User": "Alice.Smith@acme.com"}))
	if err != nil {
		t.Fatalf("Identify() = %v", err)
	}
	if user != "alice.smith" {
		t.Errorf("user = %q, want the lowercased local part", user)
	}
}

func TestIdentifyRefusesAPathTraversal(t *testing.T) {
	// The user name becomes a directory name. A traversal here would let one
	// user read another's memories, or the host's files.
	for _, bad := range []string{"../root", "a/b", `a\b`, "..", ".", "a\x00b"} {
		if _, err := Identify(cfg(), req(map[string]string{"X-Forwarded-User": bad})); !errors.Is(err, ErrBadIdentity) {
			t.Errorf("Identify(%q) = %v, want ErrBadIdentity", bad, err)
		}
	}
}

func TestIdentifyRefusesAnAbsurdlyLongName(t *testing.T) {
	long := strings.Repeat("a", 200)
	if _, err := Identify(cfg(), req(map[string]string{"X-Forwarded-User": long})); !errors.Is(err, ErrBadIdentity) {
		t.Fatalf("Identify() = %v, want ErrBadIdentity", err)
	}
}

func TestIdentifyAcceptsOrdinaryNames(t *testing.T) {
	for _, ok := range []string{"alice", "alice.smith", "alice-smith", "alice_smith", "a1"} {
		if _, err := Identify(cfg(), req(map[string]string{"X-Forwarded-User": ok})); err != nil {
			t.Errorf("Identify(%q) = %v, want nil", ok, err)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./server/ -run TestIdentify -v`
Expected: FAIL — `undefined: Identify`.

- [ ] **Step 3: Write identity**

Create `mcp/server/identity.go`:

```go
package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	ErrNoIdentity  = errors.New("server: the request carries no user identity")
	ErrBadIdentity = errors.New("server: the user identity is not usable as a directory name")
)

// maxUserLen is generous for a login name and short of any filesystem limit.
const maxUserLen = 64

// Identify turns a request into a user name.
//
// The name becomes a directory, which is what isolates one person's memories
// from another's, so it is validated hard: only characters that mean exactly
// themselves in a path are allowed.
func Identify(c Config, r *http.Request) (string, error) {
	raw := strings.TrimSpace(r.Header.Get(c.IdentityHeader))
	if raw == "" {
		return "", ErrNoIdentity
	}

	// Proxies commonly pass an email. The local part is the login name.
	if at := strings.IndexByte(raw, '@'); at > 0 {
		raw = raw[:at]
	}
	user := strings.ToLower(raw)

	if len(user) > maxUserLen {
		return "", fmt.Errorf("%w: longer than %d characters", ErrBadIdentity, maxUserLen)
	}
	for _, ch := range user {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
		case ch == '.' || ch == '-' || ch == '_':
		default:
			return "", fmt.Errorf("%w: %q contains %q", ErrBadIdentity, user, ch)
		}
	}
	// "." and ".." pass the character check but are not names.
	if strings.Trim(user, ".") == "" {
		return "", fmt.Errorf("%w: %q", ErrBadIdentity, user)
	}

	return user, nil
}
```

- [ ] **Step 4: Write the failing tenant test**

Create `mcp/server/tenant_test.go`:

```go
package server

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/memory"
)

func tenants(t *testing.T) *Tenants {
	t.Helper()
	c := cfg()
	c.Root = t.TempDir()
	ts := NewTenants(c)
	t.Cleanup(func() { _ = ts.Close() })
	return ts
}

func TestForCreatesAHomeUnderTheRoot(t *testing.T) {
	ts := tenants(t)
	tn, err := ts.For("alice")
	if err != nil {
		t.Fatalf("For() = %v", err)
	}
	if !strings.HasPrefix(tn.Layout.Home, ts.cfg.Root) {
		t.Fatalf("home %q is outside the root %q", tn.Layout.Home, ts.cfg.Root)
	}
	if _, err := os.Stat(tn.Layout.Home); err != nil {
		t.Fatalf("home was not created: %v", err)
	}
}

func TestForGivesEachUserADifferentHome(t *testing.T) {
	ts := tenants(t)
	a, _ := ts.For("alice")
	b, _ := ts.For("bob")
	if a.Layout.Home == b.Layout.Home {
		t.Fatal("two users share a home directory")
	}
}

func TestForReturnsTheSameTenantForTheSameUser(t *testing.T) {
	// Opening a second store over the same directory would give two writers
	// to one git repository.
	ts := tenants(t)
	a, _ := ts.For("alice")
	again, _ := ts.For("alice")
	if a != again {
		t.Fatal("For() opened a second tenant for the same user")
	}
}

func TestOneUserCannotSeeAnothersMemories(t *testing.T) {
	// This is the whole isolation guarantee. Users are separated by
	// directory, so there is no query to forget to filter.
	ts := tenants(t)
	alice, _ := ts.For("alice")
	bob, _ := ts.For("bob")

	if _, err := alice.Store.Put(memory.Memory{
		ID: "aaa", Slug: "s", Body: "alice's secret", Scope: "personal", Author: "alice",
	}, "test: alice's memory"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := bob.Store.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("bob can see %d of alice's memories", len(got))
	}
}

func TestForRefusesAUserNameThatEscapesTheRoot(t *testing.T) {
	// Identify should already have caught this; the second check is here
	// because a path escape is the one bug that must not survive one mistake.
	ts := tenants(t)
	if _, err := ts.For("../outside"); err == nil {
		t.Fatal("For() accepted a name that escapes the root")
	}
}

func TestHomeDirectoryIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes do not apply on Windows")
	}
	ts := tenants(t)
	tn, _ := ts.For("alice")

	info, err := os.Stat(tn.Layout.Home)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("home mode = %04o; other users on the host can read it", mode)
	}
}

func TestCloseClosesEveryTenant(t *testing.T) {
	ts := tenants(t)
	if _, err := ts.For("alice"); err != nil {
		t.Fatalf("For: %v", err)
	}
	if err := ts.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if _, err := os.Stat(filepath.Join(ts.cfg.Root, "alice")); err != nil {
		t.Errorf("closing removed the data: %v", err)
	}
}
```

- [ ] **Step 5: Run it to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./server/ -run TestFor -v`
Expected: FAIL — `undefined: NewTenants`.

- [ ] **Step 6: Write the tenants**

Create `mcp/server/tenant.go`:

```go
package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/AndrewSkea/team-memory/mcp/paths"
	"github.com/AndrewSkea/team-memory/mcp/search"
	"github.com/AndrewSkea/team-memory/mcp/staging"
	"github.com/AndrewSkea/team-memory/mcp/store"
)

// Tenant is one user's world: their own clone, their own staging area, their
// own index, their own git credential.
type Tenant struct {
	User    string
	Layout  paths.Layout
	Store   *store.Store
	Staging *staging.Area
	Index   *search.Index
}

// Tenants opens tenants on demand and keeps them open.
//
// Isolation here is by directory rather than by a user column in a shared
// database. There is no query to forget to filter, and a bug in one user's
// repository cannot reach another's.
type Tenants struct {
	cfg Config

	mu   sync.Mutex
	open map[string]*Tenant
}

func NewTenants(c Config) *Tenants {
	return &Tenants{cfg: c, open: make(map[string]*Tenant)}
}

func (t *Tenants) For(user string) (*Tenant, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if tn, ok := t.open[user]; ok {
		return tn, nil
	}

	home := filepath.Join(t.cfg.Root, user)

	// Second line of defence. Identify validates the name, but a path escape
	// is the one bug that must not survive a single mistake upstream.
	root, err := filepath.Abs(t.cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("server: resolve root: %w", err)
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return nil, fmt.Errorf("server: resolve home: %w", err)
	}
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return nil, fmt.Errorf("server: %q resolves outside the root", user)
	}

	// 0700: other accounts on the host have no business here.
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("server: create home for %s: %w", user, err)
	}

	l, err := paths.New(abs)
	if err != nil {
		return nil, fmt.Errorf("server: layout for %s: %w", user, err)
	}
	if err := l.EnsureDirs(); err != nil {
		return nil, fmt.Errorf("server: directories for %s: %w", user, err)
	}

	st, err := store.Open(l)
	if err != nil {
		return nil, fmt.Errorf("server: store for %s: %w", user, err)
	}
	sa := staging.New(l)
	ix, err := search.Open(l)
	if err != nil {
		return nil, fmt.Errorf("server: index for %s: %w", user, err)
	}

	tn := &Tenant{User: user, Layout: l, Store: st, Staging: sa, Index: ix}
	t.open[user] = tn
	return tn, nil
}

// Close releases every open tenant. The data stays on disk.
func (t *Tenants) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Only the index holds an open handle. The store is a git working copy
	// with nothing to release.
	var first error
	for _, tn := range t.open {
		if err := tn.Index.Close(); err != nil && first == nil {
			first = err
		}
	}
	t.open = make(map[string]*Tenant)
	return first
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd mcp && CGO_ENABLED=0 go test ./server/ -v`
Expected: PASS — all identity and tenant tests.

- [ ] **Step 8: Commit**

```bash
git add mcp/server/identity.go mcp/server/tenant.go mcp/server/identity_test.go mcp/server/tenant_test.go
git commit -m "feat(server): isolate users by directory with a validated identity header"
```

---

### Task 7: The server front door

**Files:**
- Create: `mcp/server/server.go`
- Test: `mcp/server/server_test.go`
- Modify: `mcp/cli/root.go` (dispatch to server mode)

**Interfaces:**
- Consumes: `Config`, `Tenants`, `review.API`, `webui.Handler`.
- Produces:
  - `func New(c Config) (*Server, error)`, `func (s *Server) Handler() http.Handler`, `func (s *Server) Run() error`, `func (s *Server) Close() error`

- [ ] **Step 1: Write the failing test**

Create `mcp/server/server_test.go`:

```go
package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newServer(t *testing.T) *Server {
	t.Helper()
	c := cfg()
	c.Root = t.TempDir()
	s, err := New(c)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func send(t *testing.T, s *Server, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	r.Host = "memory.acme.internal"
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestRequestWithNoIdentityIs401(t *testing.T) {
	s := newServer(t)
	if w := send(t, s, http.MethodGet, "/api/pending", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", w.Code, w.Body.String())
	}
}

func TestRequestWithAnIdentityIsServed(t *testing.T) {
	s := newServer(t)
	w := send(t, s, http.MethodGet, "/api/pending", map[string]string{"X-Forwarded-User": "alice"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
}

func TestAHostOutsideTheAllowlistIs403(t *testing.T) {
	// Binding a real interface does not remove the Host check; it widens it.
	s := newServer(t)
	r := httptest.NewRequest(http.MethodGet, "/api/pending", nil)
	r.Host = "evil.example.com"
	r.Header.Set("X-Forwarded-User", "alice")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestTwoUsersGetSeparateData(t *testing.T) {
	s := newServer(t)
	send(t, s, http.MethodGet, "/api/pending", map[string]string{"X-Forwarded-User": "alice"})
	send(t, s, http.MethodGet, "/api/pending", map[string]string{"X-Forwarded-User": "bob"})

	a, err := s.tenants.For("alice")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	b, err := s.tenants.For("bob")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if a.Layout.Home == b.Layout.Home {
		t.Fatal("two users share a home")
	}
}

func TestOrganiseIsRefusedInServerMode(t *testing.T) {
	// Running the user's own CLI on a shared host would put one person's
	// session text through another person's tooling.
	s := newServer(t)
	w := send(t, s, http.MethodPost, "/api/organise/propose", map[string]string{"X-Forwarded-User": "alice"})
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501: %s", w.Code, w.Body.String())
	}
}

func TestHookIsRefusedInServerMode(t *testing.T) {
	s := newServer(t)
	w := send(t, s, http.MethodPost, "/hook", map[string]string{"X-Forwarded-User": "alice"})
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", w.Code)
	}
}

func TestTheUIIsServed(t *testing.T) {
	s := newServer(t)
	w := send(t, s, http.MethodGet, "/", map[string]string{"X-Forwarded-User": "alice"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestAnIdentityThatEscapesThePathIs401(t *testing.T) {
	s := newServer(t)
	w := send(t, s, http.MethodGet, "/api/pending", map[string]string{"X-Forwarded-User": "../etc"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if strings.Contains(w.Body.String(), "etc") {
		t.Error("the response echoes the rejected identity back to the caller")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd mcp && CGO_ENABLED=0 go test ./server/ -run TestRequest -v`
Expected: FAIL — `undefined: New` for the server type.

- [ ] **Step 3: Write the server**

Create `mcp/server/server.go`:

```go
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/review"
	"github.com/AndrewSkea/team-memory/mcp/webui"
)

// Server is team-memory as a shared service.
//
// It is the same code as local mode with a different front door: identity
// comes from the proxy instead of a bearer token, every user gets their own
// directory, and everything that would run a local agent is switched off.
type Server struct {
	cfg     Config
	tenants *Tenants
	http    *http.Server
}

func New(c Config) (*Server, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &Server{cfg: c, tenants: NewTenants(c)}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Local-only features are refused explicitly rather than 404ing, so an
	// operator reading the logs can tell a missing route from a disabled one.
	mux.HandleFunc("/hook", notInServerMode("Session hooks run on your own computer, not on the shared service."))
	mux.HandleFunc("/api/organise/", notInServerMode("Organising runs a coding agent on your own computer. Use the local app for this."))

	mux.HandleFunc("/api/", s.perUser)
	mux.Handle("/", webui.Handler())

	return s.checkHost(mux)
}

// checkHost keeps the DNS-rebinding protection from local mode. Binding a
// real interface widens the allowlist; it never removes the check.
func (s *Server) checkHost(next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(s.cfg.AllowedHosts))
	for _, h := range s.cfg.AllowedHosts {
		allowed[strings.ToLower(h)] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host[i:], "]") {
			host = host[:i]
		}
		if !allowed[strings.ToLower(host)] {
			http.Error(w, "host not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// perUser resolves the caller, then hands the request to that user's own API.
func (s *Server) perUser(w http.ResponseWriter, r *http.Request) {
	user, err := Identify(s.cfg, r)
	if err != nil {
		// Deliberately terse, and it does not echo what was sent: an error
		// page is not a place to reflect attacker-controlled text.
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "This request carries no valid user identity. Sign in through your organisation's login.",
		})
		return
	}

	tn, err := s.tenants.For(user)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Could not open your memory store.",
		})
		return
	}

	// push is false: in server mode the operator decides when to publish, and
	// a push per approval from a shared host is a stampede on one remote.
	api := review.New(tn.Store, tn.Staging, tn.Index, tn.Layout, false)
	api.Handler().ServeHTTP(w, r)
}

func notInServerMode(message string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": message})
	}
}

func (s *Server) Run() error {
	s.http = &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

func (s *Server) Close() error {
	if s.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.http.Shutdown(ctx)
	}
	return s.tenants.Close()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

`TestTheUIIsServed` passes with or without an embedded bundle: the no-bundle
handler from Plan 4 Task 8 also answers 200.

- [ ] **Step 4: Dispatch to server mode from the CLI**

In `cmdServe` in `mcp/cli/root.go`, before anything else:

```go
	if server.IsServerMode(os.Getenv) {
		cfg, err := server.LoadConfig(os.Getenv)
		if err != nil {
			fmt.Fprintf(stderr, "team-memory: %v\n", err)
			return 1
		}
		s, err := server.New(cfg)
		if err != nil {
			fmt.Fprintf(stderr, "team-memory: %v\n", err)
			return 1
		}
		defer func() { _ = s.Close() }()

		fmt.Fprintf(stdout, "team-memory serving on %s for users named by %s\n",
			cfg.Addr, cfg.IdentityHeader)
		if err := s.Run(); err != nil {
			fmt.Fprintf(stderr, "team-memory: %v\n", err)
			return 1
		}
		return 0
	}
```

Server mode deliberately shares the `serve` command rather than adding a flag:
the same container image and the same command line run in both modes, and only
the environment differs.

- [ ] **Step 5: Run every test**

Run: `cd mcp && CGO_ENABLED=0 go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add mcp/server/server.go mcp/server/server_test.go mcp/cli/root.go
git commit -m "feat(server): serve per-user memory behind a proxy-set identity header"
```

---

### Task 8: Deployment documentation

**Files:**
- Create: `docs/deploying.md`
- Modify: `README.md`

**Interfaces:**
- Consumes: everything above.
- Produces: documentation. No code.

- [ ] **Step 1: Write the deployment guide**

Create `docs/deploying.md`:

````markdown
# Running team-memory for a company

team-memory runs in two modes from one binary.

**Local mode** is the default. It runs on one person's computer, binds
loopback only, and uses that person's own coding agent to read their own
sessions. Nothing leaves the machine until they approve it.

**Server mode** runs the same binary as a shared service. It never runs a
coding agent, so it never reads anyone's session transcripts. What it does is
hold each person's approved memories, show them the review queue their laptop
filled, and publish to the team repository.

## What server mode does not do

- It does not extract memories. That happens on each person's own machine.
- It does not organise memories. That runs a coding agent, which would mean
  running one person's tooling over another person's text.
- It does not authenticate anyone. Your reverse proxy does that.

## Configuration

Server mode is configured entirely by environment variables, because it runs
in a container.

| Variable | Required | Meaning |
|---|---|---|
| `TEAM_MEMORY_MODE` | yes | Set to `server`. Anything else is local mode. |
| `TEAM_MEMORY_IDENTITY_HEADER` | yes | The header your proxy sets to the logged-in user, e.g. `X-Forwarded-User`. |
| `TEAM_MEMORY_ROOT` | yes | Directory holding one subdirectory per user. Must be persistent. |
| `TEAM_MEMORY_ALLOWED_HOSTS` | yes | Comma-separated hostnames this service answers on. `*` is refused. |
| `TEAM_MEMORY_ADDR` | no | Listen address. Defaults to `:8080`. |
| `TEAM_MEMORY_DEFAULT_REPO` | no | Repository offered to new users in Settings. |

The service refuses to start if any required variable is missing. There are no
defaults for identity or hosts: a default would authenticate everyone as the
same person, or would remove the protection against DNS rebinding.

## The proxy is the security boundary

team-memory trusts the identity header completely. That is only safe if the
proxy in front of it **always** sets that header and **always** strips any
copy the client sent.

With nginx and an LDAP or OIDC auth subrequest:

```nginx
location / {
    auth_request /_auth;
    auth_request_set $user $upstream_http_x_auth_user;

    # Strip anything the client sent, then set it from the auth result.
    proxy_set_header X-Forwarded-User "";
    proxy_set_header X-Forwarded-User $user;

    proxy_set_header Host $host;
    proxy_pass http://team-memory:8080;
}
```

If the service is reachable without going through the proxy, anyone who can
reach it can claim to be anyone. Bind it to an internal network and do not
publish the port.

## Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: team-memory
spec:
  replicas: 1 # see below
  selector:
    matchLabels: { app: team-memory }
  template:
    metadata:
      labels: { app: team-memory }
    spec:
      containers:
        - name: team-memory
          image: ghcr.io/andrewskea/team-memory:2
          args: ["serve"]
          env:
            - { name: TEAM_MEMORY_MODE, value: server }
            - { name: TEAM_MEMORY_IDENTITY_HEADER, value: X-Forwarded-User }
            - { name: TEAM_MEMORY_ROOT, value: /data }
            - { name: TEAM_MEMORY_ALLOWED_HOSTS, value: memory.acme.internal }
          ports: [{ containerPort: 8080 }]
          volumeMounts:
            - { name: data, mountPath: /data }
          securityContext:
            runAsNonRoot: true
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
      volumes:
        - name: data
          persistentVolumeClaim: { claimName: team-memory-data }
```

**One replica.** Each user's data is a git working copy on disk, and two
processes writing one working copy corrupt it. Scaling out needs
per-user sharding, which this version does not do.

## Storage

Every user gets `$TEAM_MEMORY_ROOT/<user>/`, created with mode 0700, holding
their own clone, staging area, search index and git credential. Users are
separated by directory, not by a column in a shared table, so there is no
query that can be written wrongly and leak one person's memories to another.

Back up `$TEAM_MEMORY_ROOT`. Everything approved is also in the team git
repository, but staged and rejected memories exist only here — which is the
point: they were never published.

## What each user still does themselves

1. Installs team-memory on their own machine and runs `team-memory install claude`.
2. Chooses their memory level. Nobody can raise it for them, including an
   administrator: there is no setting for that, deliberately.
3. Reviews and approves their own memories.

The shared service holds the result. It never sees the sessions.
````

- [ ] **Step 2: Point the README at it**

Add to `README.md`, after the installation section:

```markdown
## Running it for a team

team-memory works on one laptop with no server at all. If your company wants a
shared instance as well, see [docs/deploying.md](docs/deploying.md) — the same
binary runs as a multi-user service behind your existing login, and it still
never reads anybody's sessions.
```

- [ ] **Step 3: Commit**

`docs/` is gitignored in this repository, so this needs `-f`:

```bash
git add -f docs/deploying.md
git add README.md
git commit -m "docs: explain how to run team-memory as a shared service"
```

---

### Task 9: Delivery — remove v1, fix the release pipeline, and test the queue end to end

**Files:**
- Delete: `scripts/copy-prompts.sh`, `mcp/frontend/`, `mcp/hook/`, `mcp/llm/`, `mcp/github/`, `mcp/config/`, `mcp/embed.go`, `mcp/stdio.go`, `mcp/main.go`, `mcp/main_test.go`, `mcp/mcp_protocol_test.go`, `frontend/`, `prompts/`, `tests/frontend/`
- Modify: `Makefile`, `.goreleaser.yaml`, `.github/workflows/release.yml`, `.github/workflows/ci.yml`, `playwright.config.js`, `package.json`
- Create: `tests/e2e/review.spec.js`

**Interfaces:**
- Consumes: everything built in Plans 1–5. This task adds no Go surface.
- Produces: a release that builds the frontend, and an end-to-end test of the
  review queue.

This is spec §10.3, §10.4 and §10.5. It is last because deleting v1 is only
safe once v2 replaces every part of it.

- [ ] **Step 1: Delete v1**

```bash
git rm -r --cached mcp/frontend mcp/hook mcp/llm mcp/github mcp/config frontend prompts tests/frontend 2>/dev/null || true
rm -rf mcp/frontend mcp/hook mcp/llm mcp/github mcp/config frontend prompts tests/frontend
rm -f mcp/embed.go mcp/stdio.go mcp/main.go mcp/main_test.go mcp/mcp_protocol_test.go scripts/copy-prompts.sh
```

`mcp/main.go` is replaced by a new one that does nothing but call the CLI:

```go
package main

import (
	"os"

	"github.com/AndrewSkea/team-memory/mcp/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
```

- [ ] **Step 2: Prove nothing references what was deleted**

Run:

```bash
cd mcp && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./...
```

Expected: both succeed. Then:

```bash
grep -rn "copy-prompts\|mcp/frontend\|tryListen\|UNSURE" \
  --include=*.go --include=*.yaml --include=*.yml --include=Makefile . \
  | grep -v docs/
```

Expected: no output. Anything that appears is a reference to something that no
longer exists, and the build only passed because it lives in a file that is
never compiled.

- [ ] **Step 3: Rewrite the Makefile**

`prompts` is gone as a concept: prompts are embedded from where they live.

```makefile
ifeq ($(OS),Windows_NT)
  EXT := .exe
else
  EXT :=
endif

.PHONY: build web install test test-go test-web test-e2e test-all

web:
	cd web && npm ci && npm run build
	rm -rf mcp/webui/build
	mkdir -p mcp/webui/build
	cp -R web/build/. mcp/webui/build/

build: web
	cd mcp && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o team-memory$(EXT) .

install: build
	mkdir -p ~/bin
	cp mcp/team-memory$(EXT) ~/bin/
	@echo "Installed to ~/bin/team-memory$(EXT)"
	@echo "Then run: team-memory install claude"

test-go:
	cd mcp && CGO_ENABLED=0 go test ./...

test-web:
	cd web && npm test

test-e2e: build
	npx playwright test

test: test-web test-go

test-all: test test-e2e
```

- [ ] **Step 4: Give the release pipeline a Node step**

Replace the `before` block and `builds` id in `.goreleaser.yaml`. The rest of
the file is unchanged:

```yaml
before:
  hooks:
    - bash -c "cd web && npm ci && npm run build"
    - bash -c "rm -rf mcp/webui/build && mkdir -p mcp/webui/build && cp -R web/build/. mcp/webui/build/"

builds:
  - id: team-memory
    dir: mcp
    binary: team-memory
    env:
      - CGO_ENABLED=0
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    ignore:
      - goos: windows
        goarch: arm64
    flags:
      - -trimpath
    ldflags:
      - -s -w -X main.version={{.Version}}
```

Update `archives.name_template` to `team-memory_{{ .Os }}_{{ .Arch }}` to match
the new binary name, and add Node to `.github/workflows/release.yml` between
the checkout and the GoReleaser step:

```yaml
      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: web/package-lock.json
```

Without this the release fails the first time goreleaser reaches the `before`
hook, because the runner has no `npm` step configured for `web/`.

Add the same Node step to `.github/workflows/ci.yml`, plus the two test
commands:

```yaml
      - run: cd web && npm ci && npm test
      - run: cd mcp && CGO_ENABLED=0 go test ./...
```

- [ ] **Step 5: Write the failing end-to-end test**

Rewrite `playwright.config.js`. The daemon writes its port and token to
`daemon.json` in `$TEAM_MEMORY_HOME`, so the test reads the token from there
rather than being given one:

```js
import { defineConfig } from '@playwright/test';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const PORT = 19438;
const HOME = mkdtempSync(join(tmpdir(), 'team-memory-e2e-'));
const binary = process.platform === 'win32' ? 'mcp\\team-memory.exe' : './mcp/team-memory';

export default defineConfig({
  testDir: './tests/e2e',
  timeout: 30_000,
  retries: 1,

  webServer: {
    command: `${binary} serve --addr 127.0.0.1:${PORT}`,
    // A temporary home keeps the test away from the developer's real
    // memories, which are in a real git repository.
    env: { TEAM_MEMORY_HOME: HOME },
    url: `http://127.0.0.1:${PORT}/`,
    reuseExistingServer: false,
    timeout: 15_000,
  },

  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    headless: true,
  },

  reporter: [['list'], ['html', { open: 'never' }]],
});

export { HOME, PORT };
```

Create `tests/e2e/review.spec.js`:

```js
import { expect, test } from '@playwright/test';
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { HOME } from '../../playwright.config.js';

function token() {
  return JSON.parse(readFileSync(join(HOME, 'daemon.json'), 'utf8')).Token;
}

function stage(id, body) {
  const dir = join(HOME, 'staging', 'pending');
  mkdirSync(dir, { recursive: true });
  writeFileSync(
    join(dir, `${id}.md`),
    `---\nid: ${id}\nscope: team\nauthor: e2e\ncreated: 2026-08-22T00:00:00Z\n---\n\n${body}\n`
  );
}

async function open(page) {
  await page.goto(`/#token=${token()}`);
}

test('the queue is empty and says so', async ({ page }) => {
  await open(page);
  await expect(page.getByText(/nothing to review/i)).toBeVisible();
});

test('the token never lands in storage', async ({ page }) => {
  // The whole point of the fragment: nothing that survives the tab.
  await open(page);
  await expect(page.getByText(/nothing to review/i)).toBeVisible();

  const stored = await page.evaluate(() => ({
    local: JSON.stringify(localStorage),
    session: JSON.stringify(sessionStorage),
    cookie: document.cookie,
  }));
  expect(stored.local).not.toContain(token());
  expect(stored.session).not.toContain(token());
  expect(stored.cookie).not.toContain(token());
});

test('the token is cleared from the address bar', async ({ page }) => {
  await open(page);
  await expect(page.getByText(/nothing to review/i)).toBeVisible();
  expect(page.url()).not.toContain('token=');
});

test('approving a memory moves it out of the queue and into browse', async ({ page }) => {
  stage('e2e-approve', 'The team deploys on Tuesdays.');
  await open(page);

  await expect(page.getByText(/deploys on Tuesdays/i)).toBeVisible();
  await page.getByRole('button', { name: /^approve$/i }).click();
  await expect(page.getByText(/nothing to review/i)).toBeVisible();

  await page.getByRole('link', { name: /browse/i }).click();
  await expect(page.getByText(/deploys on Tuesdays/i)).toBeVisible();
});

test('rejecting a memory moves it to the bin and back', async ({ page }) => {
  stage('e2e-reject', 'A thing that should not be kept.');
  await open(page);

  await page.getByRole('button', { name: /^reject$/i }).click();
  await expect(page.getByText(/nothing to review/i)).toBeVisible();

  await page.getByRole('link', { name: /bin/i }).click();
  await expect(page.getByText(/should not be kept/i)).toBeVisible();

  await page.getByRole('button', { name: /restore/i }).click();
  await page.getByRole('link', { name: /review/i }).click();
  await expect(page.getByText(/should not be kept/i)).toBeVisible();
});

test('editing before approving keeps the edit', async ({ page }) => {
  stage('e2e-edit', 'Deploys happen on tuesday');
  await open(page);

  await page.getByRole('button', { name: /^edit$/i }).click();
  const body = page.getByRole('textbox', { name: /body/i });
  await body.fill('Deploys happen on Tuesday.');
  await page.getByRole('button', { name: /save and approve/i }).click();

  await page.getByRole('link', { name: /browse/i }).click();
  await expect(page.getByText('Deploys happen on Tuesday.')).toBeVisible();
});

test('a page with no token cannot read the queue', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText(/cannot reach team-memory|no access/i)).toBeVisible();
});
```

- [ ] **Step 6: Run it**

Run: `make test-e2e`
Expected: PASS — seven tests.

If the run cannot find a browser, `npx playwright install chromium` once. Add
that line to `.github/workflows/ci.yml` before the e2e step if CI runs it.

- [ ] **Step 7: Prove the installed binary works end to end, without a browser**

The Playwright tests drive the UI. They do not prove that the thing a user
actually installs works, and they cannot run in an environment with no browser.
This test does, and it is the regression net that makes later refactoring safe:
it exercises the real binary over the real on-disk layout, through every stage a
memory passes on its way from a hook to a commit.

Create `mcp/e2e/install_test.go`. It builds the binary into `t.TempDir()`, points
`TEAM_MEMORY_HOME` at another `t.TempDir()`, and drives it as a subprocess — no
package under test is imported, so nothing can pass because of a shared in-process
shortcut.

```go
package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// build compiles the real binary once and returns its path.
func build(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "team-memory")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = ".."
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v
%s", err, out)
	}
	return bin
}

type cli struct {
	t    *testing.T
	bin  string
	home string
}

func newCLI(t *testing.T) *cli {
	t.Helper()
	return &cli{t: t, bin: build(t), home: t.TempDir()}
}

// run executes the binary and fails the test if it does not exit 0.
func (c *cli) run(args ...string) string {
	c.t.Helper()
	out, err := c.try(args...)
	if err != nil {
		c.t.Fatalf("%v: %v
%s", args, err, out)
	}
	return out
}

func (c *cli) try(args ...string) (string, error) {
	c.t.Helper()
	cmd := exec.Command(c.bin, args...)
	cmd.Env = append(os.Environ(), "TEAM_MEMORY_HOME="+c.home)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestInstallThenApproveReachesAGitCommit(t *testing.T) {
	c := newCLI(t)

	// 1. A fresh machine: init must create the layout from nothing.
	c.run("init", "--local")

	// 2. A hook captures a memory. It goes to the queue, NOT to the store.
	c.run("capture", "--body", "The team deploys on Tuesdays.", "--author", "andrew")

	var pending []struct{ ID, Body string }
	if err := json.Unmarshal([]byte(c.run("pending", "--json")), &pending); err != nil {
		t.Fatalf("pending --json: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}

	// 3. Nothing captured is in git yet. This is the product's whole promise.
	if out := c.run("log", "--oneline"); strings.Contains(out, "Tuesdays") {
		t.Fatal("captured memory reached git before anyone approved it")
	}

	// 4. Approve it, and only now does it become a commit.
	c.run("approve", pending[0].ID)
	if out := c.run("log", "--oneline"); !strings.Contains(out, "memory:") {
		t.Fatalf("approve produced no commit:
%s", out)
	}

	// 5. It is findable, and the queue is empty again.
	if out := c.run("search", "Tuesdays"); !strings.Contains(out, "Tuesdays") {
		t.Fatalf("approved memory is not searchable:
%s", out)
	}
	if out := c.run("pending", "--json"); strings.TrimSpace(out) != "[]" {
		t.Fatalf("queue not empty after approve: %s", out)
	}
}

func TestRejectKeepsItOutOfGitAndRestoreBringsItBack(t *testing.T) {
	c := newCLI(t)
	c.run("init", "--local")
	c.run("capture", "--body", "A thing that should not be kept.", "--author", "andrew")

	var pending []struct{ ID string }
	_ = json.Unmarshal([]byte(c.run("pending", "--json")), &pending)

	c.run("reject", pending[0].ID, "--reason", "wrong")

	// A rejected memory must never appear in history, under any command.
	if out := c.run("log", "--oneline"); strings.Contains(out, "should not be kept") {
		t.Fatal("rejected memory reached git")
	}
	if out := c.run("search", "should not be kept"); strings.Contains(out, "should not be kept") {
		t.Fatal("rejected memory is searchable")
	}

	// And the human can change their mind.
	c.run("restore", pending[0].ID)
	if out := c.run("pending", "--json"); !strings.Contains(out, pending[0].ID) {
		t.Fatalf("restore did not return it to the queue: %s", out)
	}
}

func TestMigrateIsSafeToRunTwice(t *testing.T) {
	c := newCLI(t)
	c.run("init", "--local")

	v1 := filepath.Join(c.home, "UNSURE.md")
	body := "### Entry: Deploys

Deploys happen on Tuesday.
"
	if err := os.WriteFile(v1, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	c.run("migrate", "--from", v1)
	first := c.run("pending", "--json")

	c.run("migrate", "--from", v1)
	if second := c.run("pending", "--json"); second != first {
		t.Fatalf("second migrate changed the queue:
first:  %s
second: %s", first, second)
	}
}
```

Run: `cd mcp && CGO_ENABLED=0 go test ./e2e/ -v`
Expected: PASS — three tests.

If a subcommand this test names does not exist under exactly that name, the test
is right and the CLI is wrong: these are the operations the product is for.
Reconcile by fixing the CLI, not by renaming the test.

Add it to the Makefile so it cannot be forgotten:

```makefile
test-integration:
	cd mcp && CGO_ENABLED=0 go test ./e2e/ -count=1

test: test-web test-go test-integration
```

- [ ] **Step 8: Run everything**

Run: `make test-all`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "chore: remove v1, build the frontend in CI, and test the queue end to end"
```

---

## Definition of done for Plan 5

- [ ] `cd mcp && CGO_ENABLED=0 go test ./...` passes.
- [ ] `cd web && npm test` passes.
- [ ] `TestApproveIsTheOnlyCallerOfStorePut` passes with exactly two exempted files: `review/approve.go` and `reorg/api.go`.
- [ ] Proposing changes nothing: `TestProposeDoesNotChangeTheStore` passes.
- [ ] A proposal naming a memory that does not exist is refused before the user sees it.
- [ ] Applying a proposal against a store that moved returns 409 and applies nothing.
- [ ] Server mode refuses to start without an identity header, a root, or a host allowlist.
- [ ] Server mode returns 501 for `/hook` and for every `/api/organise/` route.
- [ ] Two users in server mode get different directories, and neither can read the other's memories.
- [ ] `grep -rn "os/exec" mcp/server/` finds nothing.
- [ ] Nothing in the repository references `copy-prompts`, `mcp/frontend`, `tryListen` or `UNSURE` any more.
- [ ] `.goreleaser.yaml` builds the frontend before the Go build, and `release.yml` sets up Node.
- [ ] `make test-all` passes, including the seven browser end-to-end tests and the three binary-level integration tests.
- [ ] `cd mcp && CGO_ENABLED=0 go test ./e2e/` passes with no browser installed.

## What Plan 5 deliberately does not do

- No horizontal scaling. One replica, stated in the deployment doc, because a git working copy has one writer.
- No LDAP or OIDC client. The proxy authenticates; team-memory only reads the result.
- No administrative override of anyone's memory level. There is no such setting, and adding one would make the privacy promise conditional on an administrator.
- No per-team policy enforcement beyond the `policy.yml` floor already in the storage layer.
- No phone or mobile client.
