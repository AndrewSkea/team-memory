package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/internal/fakegh"
	"github.com/AndrewSkea/team-memory/mcp/internal/testenv"
	"github.com/AndrewSkea/team-memory/mcp/server"
)

func TestFrontendEmbedded(t *testing.T) {
	env := testenv.New(t, nil, false)
	h := server.New(env.App, "test", frontend()).Handler()
	for path, want := range map[string]string{"/": "Team Memory", "/src/app.js": "import", "/src/ui/main.css": "{"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.Host = "127.0.0.1:7438"
		h.ServeHTTP(w, req)
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Errorf("GET %s = %d", path, w.Code)
		}
	}
}

func TestRunBasics(t *testing.T) {
	t.Setenv("TEAM_MEMORY_CONFIG", filepath.Join(t.TempDir(), "c.json"))
	t.Setenv("TEAM_MEMORY_DATA", t.TempDir())
	if run([]string{"version"}) != 0 || run([]string{"help"}) != 0 {
		t.Fatal("version/help")
	}
	if run([]string{"bogus"}) != 2 || run([]string{"hook"}) != 2 {
		t.Fatal("usage errors")
	}
	// Hooks never fail the session, even unconfigured.
	if run([]string{"hook", "prompt-submit"}) != 0 {
		t.Fatal("hook unconfigured")
	}
}

// buildBinary compiles the real binary once for the end-to-end test.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "team-memory-mcp")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

type harness struct {
	t    *testing.T
	bin  string
	env  []string
	home string
}

func (h harness) run(stdin string, args ...string) (string, string, int) {
	h.t.Helper()
	cmd := exec.Command(h.bin, args...)
	cmd.Env = h.env
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		h.t.Fatal(err)
	}
	return out.String(), errb.String(), code
}

func TestBinaryEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses a bash fake of the claude CLI")
	}
	fake := fakegh.New("acme", "kb", "tok")
	fake.Login = "alice"
	srv := httptest.NewServer(fake)
	defer srv.Close()

	home := t.TempDir()
	fakeClaude := filepath.Join(home, "fake-claude")
	extraction := `{"memories":[{"title":"Payments API rate-limits at 100 rps per key","summary":"Retry 429s with jittered backoff.","body":"Immediate retries amplify throttling.","about":["payments-api"],"tags":["rate-limit"],"type":"gotcha","sensitivity":"shareable","confidence":0.95,"supersedes":[]}],"new_entities":[{"name":"payments-api","aliases":["PAYMENTS_API_URL"],"description":"Card payments"}],"corrections":[]}`
	classification := `{"title":"Deploys need migrations first","summary":"Run migrations before deploying.","about":["deploy-pipeline"],"tags":["deploy"],"type":"howto","new_entities":[]}`
	script := "#!/bin/sh\nprompt=$(cat)\ncase \"$prompt\" in\n  *'You extract durable'*) text='" + extraction + "';;\n  *) text='" + classification + "';;\nesac\n" +
		"python3 -c 'import json,sys; print(json.dumps({\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":sys.argv[1]}]}}))' \"$text\"\n"
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	h := harness{t: t, bin: buildBinary(t), home: home, env: append(os.Environ(),
		"HOME="+home, "USERPROFILE="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"TEAM_MEMORY_API_URL="+srv.URL, "TEAM_MEMORY_CLAUDE="+fakeClaude, "TEAM_MEMORY_NO_CLAUDE_CLI=1",
		"CLAUDE_CONFIG_DIR="+filepath.Join(home, ".claude"), "TEAM_MEMORY_CONFIG=", "TEAM_MEMORY_DATA=", "GITHUB_TOKEN=")}

	// Install non-interactively.
	_, stderr, code := h.run("", "install", "--repo", "acme/kb", "--token", "tok", "--yes", "--no-service")
	if code != 0 || !strings.Contains(stderr, "Connected to acme/kb as alice") {
		t.Fatalf("install (%d):\n%s", code, stderr)
	}
	settings, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "PreCompact", "SessionEnd"} {
		if !strings.Contains(string(settings), ev) {
			t.Fatalf("settings missing %s:\n%s", ev, settings)
		}
	}
	claudeJSON, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	if !strings.Contains(string(claudeJSON), h.bin) {
		t.Fatalf("MCP not registered:\n%s", claudeJSON)
	}
	cfgInfo, err := os.Stat(filepath.Join(home, ".config", "team-memory", "config.json"))
	if err != nil || cfgInfo.Mode().Perm() != 0o600 {
		t.Fatalf("config: %v %v", err, cfgInfo)
	}

	// A session in a billing repo ends: SessionEnd returns fast and a
	// detached worker captures knowledge in the background.
	billing := filepath.Join(home, "billing-service")
	os.MkdirAll(filepath.Join(billing, ".git"), 0o755)
	os.WriteFile(filepath.Join(billing, ".git", "config"), []byte("[remote \"origin\"]\n\turl = git@github.com:acme/billing-service.git\n"), 0o644)
	transcript := filepath.Join(home, "t.jsonl")
	var tb strings.Builder
	for _, l := range []string{
		`{"type":"user","message":{"role":"user","content":"Checkout fails with 429s from the payments API during the sale. Why? ` + strings.Repeat("ctx ", 60) + `"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"The payments API allows 100 rps per key and our retries were immediate. ` + strings.Repeat("detail ", 60) + `"}]}}`,
	} {
		tb.WriteString(l + "\n")
	}
	os.WriteFile(transcript, []byte(tb.String()), 0o644)
	ev, _ := json.Marshal(map[string]string{"session_id": "s1", "transcript_path": transcript, "cwd": billing, "hook_event_name": "SessionEnd", "reason": "exit"})
	start := time.Now()
	if _, stderr, code := h.run(string(ev), "hook", "session-end"); code != 0 {
		t.Fatalf("session-end: %d %s", code, stderr)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("SessionEnd hook took %v — must return immediately", time.Since(start))
	}
	var memPath string
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline) && memPath == ""; time.Sleep(200 * time.Millisecond) {
		for _, p := range fake.Paths() {
			if strings.HasPrefix(p, "memories/") {
				memPath = p
			}
		}
	}
	if memPath == "" {
		logs, _ := os.ReadFile(filepath.Join(home, ".local", "share", "team-memory", "logs", "team-memory.log"))
		wlogs, _ := os.ReadFile(filepath.Join(home, ".local", "share", "team-memory", "logs", "worker.log"))
		t.Fatalf("background capture never published.\nlog:\n%s\nworker:\n%s", logs, wlogs)
	}
	content, _ := fake.File(memPath)
	if !strings.Contains(content, "learned_in: acme/billing-service") {
		t.Fatalf("memory:\n%s", content)
	}

	// A new session in a *different* repo that uses the payments API gets it.
	checkout := filepath.Join(home, "checkout-web")
	os.MkdirAll(filepath.Join(checkout, ".git"), 0o755)
	os.WriteFile(filepath.Join(checkout, ".git", "config"), []byte("[remote \"origin\"]\n\turl = git@github.com:acme/checkout-web.git\n"), 0o644)
	os.WriteFile(filepath.Join(checkout, ".env.example"), []byte("PAYMENTS_API_URL=\n"), 0o644)
	ev, _ = json.Marshal(map[string]string{"session_id": "s2", "cwd": checkout, "hook_event_name": "SessionStart", "source": "startup"})
	out, stderr, code := h.run(string(ev), "hook", "session-start")
	if code != 0 || !strings.Contains(out, "Payments API rate-limits") || !strings.Contains(out, `"hookEventName":"SessionStart"`) {
		t.Fatalf("session-start (%d): %s %s", code, out, stderr)
	}
	ev, _ = json.Marshal(map[string]string{"session_id": "s3", "cwd": home, "hook_event_name": "UserPromptSubmit", "prompt": "how should I handle 429 errors from the payments api?"})
	out, _, _ = h.run(string(ev), "hook", "prompt-submit")
	if !strings.Contains(out, "additionalContext") || !strings.Contains(out, "rate-limits") {
		t.Fatalf("prompt-submit: %s", out)
	}
	// Nested sessions (our own claude -p) are ignored.
	cmd := exec.Command(h.bin, "hook", "prompt-submit")
	cmd.Env = append(h.env, "TEAM_MEMORY_NESTED=1")
	cmd.Stdin = strings.NewReader(string(ev))
	if b, _ := cmd.Output(); len(b) != 0 {
		t.Fatalf("nested: %s", b)
	}

	// CLI add/search.
	if out, stderr, code := h.run("", "add", "always run db migrations before deploy"); code != 0 || !strings.Contains(out, "Deploys need migrations first") {
		t.Fatalf("add: %s %s", out, stderr)
	}
	if out, _, _ := h.run("", "search", "migrations deploy"); !strings.Contains(out, "Deploys need migrations first") {
		t.Fatalf("search: %s", out)
	}
	if out, _, _ := h.run("", "mode", "review"); !strings.Contains(out, "Capture mode: review") {
		t.Fatalf("mode: %s", out)
	}

	// MCP over stdio (web UI disabled so the test doesn't grab port 7438).
	mcpIn := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"recall","arguments":{"query":"payments 429"}}}` + "\n"
	out, _, code = h.run(mcpIn, "mcp", "--no-http")
	if code != 0 || !strings.Contains(out, `"protocolVersion":"2025-06-18"`) || !strings.Contains(out, "rate-limits") {
		t.Fatalf("mcp: %s", out)
	}

	// Web UI serves the same data.
	port := freePort(t)
	srvCmd := exec.Command(h.bin, "serve", "--port", port)
	srvCmd.Env = h.env
	if err := srvCmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer srvCmd.Process.Kill()
	var body []byte
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		resp, err := http.Get("http://127.0.0.1:" + port + "/v1/memories?q=payments")
		if err == nil {
			body, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
			break
		}
	}
	if !strings.Contains(string(body), "rate-limits") {
		t.Fatalf("web API: %s", body)
	}

	// Doctor passes (hooks, MCP, access, sync).
	if out, _, code := h.run("", "doctor"); code != 0 {
		// LLM check uses TEAM_MEMORY_CLAUDE, so everything should be green.
		t.Fatalf("doctor (%d):\n%s", code, out)
	}

	// Uninstall removes our wiring but leaves the repo alone.
	h.run("", "uninstall")
	settings, _ = os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if strings.Contains(string(settings), "team-memory-mcp") {
		t.Fatalf("uninstall left hooks:\n%s", settings)
	}
}

func freePort(t *testing.T) string {
	ln := httptest.NewServer(http.NotFoundHandler())
	addr := ln.Listener.Addr().String()
	ln.Close()
	return addr[strings.LastIndex(addr, ":")+1:]
}
