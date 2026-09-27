package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestRunClaude_PromptOnStdinAndIsolated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash fake")
	}
	c := NewClaude("testdata/fake_claude_echo.sh")
	big := strings.Repeat("x", 300_000) // larger than Linux's 128KB per-arg limit
	out, err := c.Run(context.Background(), big)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	parts := strings.SplitN(out, "|", 3)
	if len(parts) != 3 || len(parts[0]) != len(big) || parts[1] != "1" {
		t.Fatalf("unexpected: %.100s", out)
	}
	for _, flag := range []string{"--strict-mcp-config", "disableAllHooks", "--no-session-persistence", "--model"} {
		if !strings.Contains(parts[2], flag) {
			t.Errorf("missing %s in args %q", flag, parts[2])
		}
	}
}

func TestExtractJSON(t *testing.T) {
	cases := map[string]string{
		"```json\n{\"a\":1}\n```":                  `{"a":1}`,
		"Sure! Here: {\"a\":{\"b\":\"}\"}} thanks": `{"a":{"b":"}"}}`,
		"no json": "no json",
	}
	for in, want := range cases {
		if got := ExtractJSON(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestAPIRunner(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" || r.URL.Path != "/v1/messages" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"content":[{"type":"text","text":"hello"}]}`))
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	out, err := NewAPI("k").Run(context.Background(), "p")
	if err != nil || out != "hello" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := NewAPI("bad").Run(context.Background(), "p"); err == nil {
		t.Fatal("expected error")
	}
}

func TestAutoFallsBack(t *testing.T) {
	t.Setenv("TEAM_MEMORY_CLAUDE", "/definitely/not/claude")
	t.Setenv("ANTHROPIC_API_KEY", "")
	if _, ok := Auto().(Unavailable); !ok {
		t.Fatal("want Unavailable")
	}
	t.Setenv("ANTHROPIC_API_KEY", "k")
	if _, ok := Auto().(*API); !ok {
		t.Fatal("want API")
	}
}
