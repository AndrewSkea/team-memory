package llm

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestRunClaude_FakeBackend(t *testing.T) {
	script := "testdata/fake_claude.sh"
	if runtime.GOOS == "windows" {
		script = "testdata\\fake_claude.cmd"
	}
	c := NewClaude(script)
	out, err := c.Run(context.Background(), "ignored prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, `"target_file"`) {
		t.Errorf("expected JSON in output, got: %s", out)
	}
}

func TestRunClaude_NotFound(t *testing.T) {
	c := NewClaude("/definitely/not/a/real/binary")
	_, err := c.Run(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}
