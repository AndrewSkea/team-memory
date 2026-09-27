package llm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// DefaultModel is the Claude model alias used for categorisation, summary and
// reminder calls. Pinned to Haiku to keep costs low — these are short,
// structured generations that do not benefit from a larger model. Override
// with the TEAM_MEMORY_MODEL env var if needed.
const DefaultModel = "haiku"

// NestedEnv is set on every `claude` child process. The hook handler exits
// early when it sees it, so the nested session's own SessionEnd/PreCompact
// hooks can't recursively summarise themselves.
const NestedEnv = "TEAM_MEMORY_NESTED"

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
		path = "claude"
	}
	model := os.Getenv("TEAM_MEMORY_MODEL")
	if model == "" {
		model = DefaultModel
	}
	return &Claude{path: path, model: model}
}

// Run invokes `claude -p <prompt> --model <model> --output-format stream-json --verbose`
// and returns the assembled assistant text.
func (c *Claude) Run(ctx context.Context, prompt string) (string, error) {
	args := append([]string{
		"-p", prompt,
		"--model", c.model,
		"--output-format", "stream-json",
		"--verbose",
	}, isolation...)
	cmd := exec.CommandContext(ctx, c.path, args...)
	cmd.Env = append(os.Environ(), NestedEnv+"=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(stderr.String())
		if len(tail) > 500 {
			tail = "..." + tail[len(tail)-500:]
		}
		return "", fmt.Errorf("claude failed: %w (stderr: %s)", err, tail)
	}
	text, err := ExtractAssistantText(&stdout)
	if err != nil {
		return "", fmt.Errorf("parse stream-json: %w", err)
	}
	return text, nil
}
