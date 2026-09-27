package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func paths(t *testing.T) Paths {
	t.Helper()
	t.Setenv("TEAM_MEMORY_NO_CLAUDE_CLI", "1")
	home := t.TempDir()
	return Paths{
		Home:       home,
		Bin:        filepath.Join(home, "my bin", "team-memory-mcp"),
		ClaudeJSON: filepath.Join(home, ".claude.json"),
		Settings:   filepath.Join(home, ".claude", "settings.json"),
		Commands:   filepath.Join(home, ".claude", "commands"),
	}
}

const existingSettings = `{
  "model": "opus",
  "hooks": {
    "Stop": [
      {"matcher": "", "hooks": [{"type": "command", "command": "team-memory-mcp --once session-end", "timeout": 60}]},
      {"matcher": "", "hooks": [{"type": "command", "command": "say done"}]}
    ],
    "PreCompact": [{"matcher": "", "hooks": [{"type": "command", "command": "/old/team-memory-mcp --once precompact"}]}],
    "PostToolUse": [{"matcher": "Edit", "hooks": [{"type": "command", "command": "prettier"}]}]
  },
  "zeta": true
}`

func TestInstallHooksIdempotentAndPreservesOthers(t *testing.T) {
	p := paths(t)
	os.MkdirAll(filepath.Dir(p.Settings), 0o755)
	os.WriteFile(p.Settings, []byte(existingSettings), 0o644)
	for i := 0; i < 2; i++ {
		if _, err := InstallHooks(p); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := os.ReadFile(p.Settings)
	s := string(raw)
	// Key order preserved.
	if strings.Index(s, `"model"`) > strings.Index(s, `"hooks"`) || strings.Index(s, `"hooks"`) > strings.Index(s, `"zeta"`) {
		t.Fatalf("key order changed:\n%s", s)
	}
	var parsed struct {
		Hooks map[string][]hookEntry `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if n := len(parsed.Hooks["Stop"]); n != 1 || parsed.Hooks["Stop"][0].Hooks[0].Command != "say done" {
		t.Fatalf("Stop: %+v", parsed.Hooks["Stop"])
	}
	if len(parsed.Hooks["PostToolUse"]) != 1 {
		t.Fatal("foreign hook removed")
	}
	for _, h := range Hooks {
		entries := parsed.Hooks[h.Event]
		if len(entries) != 1 || !strings.Contains(entries[0].Hooks[0].Command, "hook "+h.Arg) {
			t.Fatalf("%s: %+v", h.Event, entries)
		}
	}
	if runtime.GOOS != "windows" && !strings.Contains(parsed.Hooks["SessionEnd"][0].Hooks[0].Command, "'") {
		t.Fatalf("path with space must be quoted: %s", parsed.Hooks["SessionEnd"][0].Hooks[0].Command)
	}
	got, _ := HooksInstalled(p)
	if len(got) != 4 {
		t.Fatalf("installed: %v", got)
	}
	if _, err := os.Stat(p.Settings + ".team-memory-backup"); err != nil {
		t.Fatal("backup missing")
	}

	if _, err := RemoveHooks(p); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(p.Settings)
	if strings.Contains(string(raw), Marker) || !strings.Contains(string(raw), "say done") || !strings.Contains(string(raw), "prettier") {
		t.Fatalf("remove:\n%s", raw)
	}
}

func TestInstallHooksFreshAndInvalid(t *testing.T) {
	p := paths(t)
	if _, err := InstallHooks(p); err != nil {
		t.Fatal(err)
	}
	if got, _ := HooksInstalled(p); len(got) != 4 {
		t.Fatalf("%v", got)
	}
	os.WriteFile(p.Settings, []byte("{not json"), 0o644)
	if _, err := InstallHooks(p); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("got %v", err)
	}
}

func TestRegisterMCPFileFallback(t *testing.T) {
	p := paths(t)
	os.WriteFile(p.ClaudeJSON, []byte(`{"numStartups": 5, "mcpServers": {"other": {"command": "x"}, "team-memory": {"command": "old"}}, "projects": {}}`), 0o600)
	if _, err := RegisterMCP(p); err != nil {
		t.Fatal(err)
	}
	var o map[string]any
	raw, _ := os.ReadFile(p.ClaudeJSON)
	json.Unmarshal(raw, &o)
	servers := o["mcpServers"].(map[string]any)
	tm := servers["team-memory"].(map[string]any)
	if tm["command"] != p.Bin || tm["args"].([]any)[0] != "mcp" || servers["other"] == nil || o["numStartups"].(float64) != 5 {
		t.Fatalf("%s", raw)
	}
	if !MCPRegistered(p) {
		t.Fatal("not registered")
	}
	if err := UnregisterMCP(p); err != nil || MCPRegistered(p) {
		t.Fatal("unregister")
	}
	info, _ := os.Stat(p.ClaudeJSON)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions changed: %v", info.Mode())
	}
}

func TestCommands(t *testing.T) {
	p := paths(t)
	if err := InstallCommands(p); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(p.Commands, "memory-review.md"))
	if err != nil || !strings.Contains(string(b), "list_pending") {
		t.Fatal("memory-review missing")
	}
	RemoveCommands(p)
	if _, err := os.Stat(filepath.Join(p.Commands, "memory-add.md")); err == nil {
		t.Fatal("not removed")
	}
}

func TestServiceDefinitions(t *testing.T) {
	plist := LaunchdPlist("/Users/a b/bin/team-memory-mcp", 7438, "/tmp/logs", "/x:/y")
	for _, want := range []string{"<string>/Users/a b/bin/team-memory-mcp</string>", "<string>serve</string>", "<string>7438</string>", "<key>PATH</key>", "<string>/x:/y</string>"} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q", want)
		}
	}
	unit := SystemdUnit("/home/a/bin/team-memory-mcp", 7438, "/a:/b")
	if !strings.Contains(unit, `ExecStart="/home/a/bin/team-memory-mcp" serve --port 7438`) || !strings.Contains(unit, "Environment=PATH=/a:/b") {
		t.Errorf("unit:\n%s", unit)
	}
	if sp := ServicePATH("/opt/tm/team-memory-mcp"); !strings.HasPrefix(sp, "/opt/tm") {
		t.Errorf("service path %s", sp)
	}
}
