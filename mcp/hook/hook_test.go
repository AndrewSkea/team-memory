package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/app"
	"github.com/AndrewSkea/team-memory/mcp/config"
	"github.com/AndrewSkea/team-memory/mcp/llm"
)

func newApp(t *testing.T, configured bool) *app.App {
	t.Helper()
	t.Setenv("TEAM_MEMORY_NO_SPAWN", "1")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if configured {
		if err := config.Write(cfgPath, config.Config{Token: "t", Owner: "o", Repo: "r"}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := app.Open(app.Options{ConfigPath: cfgPath, DataDir: filepath.Join(dir, "data"), Runner: llm.Unavailable{},
		APIBase: "http://127.0.0.1:1", LogWriter: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestCaptureHooksEnqueueAndReturnFast(t *testing.T) {
	a := newApp(t, true)
	var out bytes.Buffer
	in := `{"session_id":"abc","transcript_path":"/tmp/t.jsonl","cwd":"/tmp","hook_event_name":"SessionEnd","reason":"exit"}`
	if err := Run(context.Background(), a, SessionEnd, strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 || a.QueueLen() != 1 {
		t.Fatalf("out=%q queue=%d", out.String(), a.QueueLen())
	}
	// Legacy/unknown payloads are tolerated.
	if err := Run(context.Background(), a, PreCompact, strings.NewReader(`not json`), &out); err != nil {
		t.Fatal(err)
	}
	if a.QueueLen() != 1 {
		t.Fatal("bad payload should not enqueue")
	}
}

func TestUnconfiguredIsSilent(t *testing.T) {
	a := newApp(t, false)
	for _, ev := range []string{SessionStart, PromptSubmit, SessionEnd, PreCompact} {
		var out bytes.Buffer
		if err := Run(context.Background(), a, ev, strings.NewReader(`{"session_id":"s","prompt":"hello there payments","transcript_path":"/x"}`), &out); err != nil {
			t.Fatalf("%s: %v", ev, err)
		}
		if out.Len() != 0 {
			t.Fatalf("%s wrote %q", ev, out.String())
		}
	}
	if a.QueueLen() != 0 {
		t.Fatal("unconfigured should not enqueue")
	}
}

func TestNestedGuard(t *testing.T) {
	a := newApp(t, true)
	t.Setenv(llm.NestedEnv, "1")
	var out bytes.Buffer
	_ = Run(context.Background(), a, SessionEnd, strings.NewReader(`{"session_id":"s","transcript_path":"/x"}`), &out)
	if a.QueueLen() != 0 {
		t.Fatal("nested session must not capture")
	}
}

func TestEmitFormat(t *testing.T) {
	var out bytes.Buffer
	if err := emit(&out, "UserPromptSubmit", app.HookOutput{Context: "ctx", SystemMessage: "msg"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	hso := got["hookSpecificOutput"].(map[string]any)
	if hso["hookEventName"] != "UserPromptSubmit" || hso["additionalContext"] != "ctx" || got["systemMessage"] != "msg" {
		t.Fatalf("%v", got)
	}
	out.Reset()
	_ = emit(&out, "SessionStart", app.HookOutput{})
	if out.Len() != 0 {
		t.Fatal("empty output should print nothing")
	}
	if err := Run(context.Background(), newApp(t, false), "bogus", strings.NewReader("{}"), &out); err == nil {
		t.Fatal("unknown event should error")
	}
}
