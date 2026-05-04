package llm

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Claude struct {
	path string
}

func NewClaude(path string) *Claude {
	if path == "" {
		path = "claude"
	}
	return &Claude{path: path}
}

// Run invokes `claude -p <prompt> --output-format stream-json --verbose` and
// returns the assembled assistant text.
func (c *Claude) Run(ctx context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, c.path,
		"-p", prompt,
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
