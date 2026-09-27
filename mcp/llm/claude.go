package llm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// DefaultModel is the Claude model alias used for extraction and
// classification. Pinned to Haiku to keep costs low — these are short,
// structured generations that do not benefit from a larger model. Override
// with the TEAM_MEMORY_MODEL env var if needed.
const DefaultModel = "haiku"

// NestedEnv is set on every `claude` child process. Hook handlers exit
// early when they see it, so the nested session's own hooks can't
// recursively capture or recall.
const NestedEnv = "TEAM_MEMORY_NESTED"

// Runner produces a completion for a prompt.
type Runner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

// isolation keeps the child `claude -p` lightweight and side-effect free:
// no user hooks, no MCP servers (which would spawn another team-memory
// process), and no saved session cluttering the user's history.
var isolation = []string{
	"--settings", `{"disableAllHooks":true}`,
	"--strict-mcp-config",
	"--no-session-persistence",
}

type Claude struct {
	path  string
	model string
}

func NewClaude(path string) *Claude {
	if path == "" {
		path = os.Getenv("TEAM_MEMORY_CLAUDE")
	}
	if path == "" {
		path = "claude"
	}
	model := os.Getenv("TEAM_MEMORY_MODEL")
	if model == "" {
		model = DefaultModel
	}
	return &Claude{path: path, model: model}
}

// Available reports whether the claude CLI can be found.
func (c *Claude) Available() bool {
	_, err := exec.LookPath(c.path)
	return err == nil
}

// Run invokes `claude -p --model <model> --output-format stream-json --verbose`
// with the prompt on stdin (argv is limited to 128KB per argument on Linux
// and 32K characters in total on Windows) and returns the assistant text.
func (c *Claude) Run(ctx context.Context, prompt string) (string, error) {
	args := append([]string{
		"-p",
		"--model", c.model,
		"--output-format", "stream-json",
		"--verbose",
	}, isolation...)
	cmd := exec.CommandContext(ctx, c.path, args...)
	cmd.Env = append(os.Environ(), NestedEnv+"=1")
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(stderr.String())
		if tail == "" {
			tail = strings.TrimSpace(stdout.String())
		}
		if len(tail) > 500 {
			tail = "..." + tail[len(tail)-500:]
		}
		return "", fmt.Errorf("claude failed: %w (%s)", err, tail)
	}
	text, err := ExtractAssistantText(&stdout)
	if err != nil {
		return "", fmt.Errorf("parse stream-json: %w", err)
	}
	return text, nil
}

// Unavailable is a Runner that always fails with a helpful message.
type Unavailable struct{}

func (Unavailable) Run(context.Context, string) (string, error) {
	return "", fmt.Errorf("no LLM available: install the Claude Code CLI (`claude`) or set ANTHROPIC_API_KEY")
}

// Auto picks the best available runner: the claude CLI (uses the user's
// Claude Code login), else the Messages API if ANTHROPIC_API_KEY is set.
func Auto() Runner {
	c := NewClaude("")
	if c.Available() {
		return c
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return NewAPI(key)
	}
	return Unavailable{}
}

// ExtractJSON returns the first balanced JSON object in s, tolerating
// markdown fences and surrounding prose.
func ExtractJSON(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return strings.TrimSpace(s)
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case ch == '\\':
				esc = true
			case ch == '"':
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return strings.TrimSpace(s[start:])
}
