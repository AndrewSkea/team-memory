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
	cmd := exec.CommandContext(ctx, c.path,
		"-p", prompt,
		"--model", c.model,
		"--output-format", "stream-json",
		"--verbose",
	)
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
