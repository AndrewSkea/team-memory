package llm

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestNewClaude_DefaultsToHaiku(t *testing.T) {
	t.Setenv("TEAM_MEMORY_MODEL", "")
	c := NewClaude("")
	if c.model != "haiku" {
		t.Errorf("expected default model 'haiku', got %q", c.model)
	}
}

func TestNewClaude_HonoursEnvOverride(t *testing.T) {
	t.Setenv("TEAM_MEMORY_MODEL", "sonnet")
	c := NewClaude("")
	if c.model != "sonnet" {
		t.Errorf("expected env override 'sonnet', got %q", c.model)
	}
}

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
