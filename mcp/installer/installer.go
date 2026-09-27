// Package installer wires team-memory into Claude Code: hooks in
// ~/.claude/settings.json, the MCP server registration, slash commands, and
// an optional background service. Everything is idempotent and reversible.
package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Marker identifies our hook commands and files.
const Marker = "team-memory-mcp"

// Hook describes one Claude Code hook we install.
type Hook struct {
	Event   string
	Arg     string
	Timeout int
}

var Hooks = []Hook{
	{"SessionStart", "session-start", 15},
	{"UserPromptSubmit", "prompt-submit", 5},
	{"PreCompact", "precompact", 10},
	{"SessionEnd", "session-end", 10},
}

// legacyEvents are events older versions hooked that we clean up.
var legacyEvents = []string{"Stop"}

type Paths struct {
	Home       string
	Bin        string // absolute path to the team-memory-mcp binary
	ClaudeJSON string // ~/.claude.json
	Settings   string // ~/.claude/settings.json
	Commands   string // ~/.claude/commands
}

func DefaultPaths(bin string) Paths {
	home, _ := os.UserHomeDir()
	claudeDir := filepath.Join(home, ".claude")
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		claudeDir = v
	}
	return Paths{
		Home:       home,
		Bin:        bin,
		ClaudeJSON: filepath.Join(home, ".claude.json"),
		Settings:   filepath.Join(claudeDir, "settings.json"),
		Commands:   filepath.Join(claudeDir, "commands"),
	}
}

// command renders the hook command line. Claude Code runs hooks through a
// shell (bash, including Git Bash on Windows), so quote accordingly.
func command(bin, arg string) string {
	if runtime.GOOS == "windows" {
		return `"` + filepath.ToSlash(bin) + `" hook ` + arg
	}
	return shellQuote(bin) + " hook " + arg
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r == '+' || r == ':' || r == '@' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

type hookEntry struct {
	Matcher *string       `json:"matcher,omitempty"`
	Hooks   []hookCommand `json:"hooks"`
}

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

func isOurs(e hookEntry) bool {
	for _, h := range e.Hooks {
		if strings.Contains(h.Command, Marker) {
			return true
		}
	}
	return false
}

// InstallHooks (re)writes our hooks, leaving everyone else's untouched.
func InstallHooks(p Paths) ([]string, error) {
	return editHooks(p, true)
}

// RemoveHooks removes our hooks.
func RemoveHooks(p Paths) ([]string, error) {
	return editHooks(p, false)
}

func editHooks(p Paths, install bool) ([]string, error) {
	settings, err := readObjectFile(p.Settings)
	if err != nil {
		return nil, err
	}
	hooks, err := settings.child("hooks")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.Settings, err)
	}
	var changes []string
	events := append([]string{}, legacyEvents...)
	for _, h := range Hooks {
		events = append(events, h.Event)
	}
	for _, ev := range events {
		raw, ok := hooks.get(ev)
		if !ok {
			continue
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, fmt.Errorf("%s: hooks.%s is not a list", p.Settings, ev)
		}
		var kept []json.RawMessage
		removed := false
		for _, e := range entries {
			var he hookEntry
			if json.Unmarshal(e, &he) == nil && isOurs(he) {
				removed = true
				continue
			}
			kept = append(kept, e)
		}
		if !removed {
			continue
		}
		if len(kept) == 0 {
			hooks.del(ev)
		} else {
			_ = hooks.setValue(ev, kept)
		}
		if contains(legacyEvents, ev) {
			changes = append(changes, "removed legacy "+ev+" hook")
		}
	}
	if install {
		for _, h := range Hooks {
			var entries []json.RawMessage
			if raw, ok := hooks.get(h.Event); ok {
				_ = json.Unmarshal(raw, &entries)
			}
			e, _ := json.Marshal(hookEntry{Hooks: []hookCommand{{Type: "command", Command: command(p.Bin, h.Arg), Timeout: h.Timeout}}})
			entries = append(entries, e)
			_ = hooks.setValue(h.Event, entries)
			changes = append(changes, "hooked "+h.Event)
		}
	} else {
		changes = append(changes, "removed hooks")
	}
	settings.setChild("hooks", hooks)
	return changes, writeObjectFile(p.Settings, settings)
}

// HooksInstalled reports which of our hook events are present.
func HooksInstalled(p Paths) (map[string]bool, error) {
	out := map[string]bool{}
	settings, err := readObjectFile(p.Settings)
	if err != nil {
		return out, err
	}
	hooks, err := settings.child("hooks")
	if err != nil {
		return out, err
	}
	for _, h := range Hooks {
		raw, ok := hooks.get(h.Event)
		if !ok {
			continue
		}
		var entries []hookEntry
		_ = json.Unmarshal(raw, &entries)
		for _, e := range entries {
			if isOurs(e) {
				out[h.Event] = true
			}
		}
	}
	return out, nil
}

// mcpEntry is the MCP server registration.
func mcpEntry(bin string) map[string]any {
	return map[string]any{"type": "stdio", "command": bin, "args": []string{"mcp"}}
}

// RegisterMCP registers the MCP server at user scope. It prefers the
// `claude mcp` CLI (which handles concurrent edits of ~/.claude.json safely)
// and falls back to editing the file.
func RegisterMCP(p Paths) (string, error) {
	entry, _ := json.Marshal(mcpEntry(p.Bin))
	if claude, err := exec.LookPath("claude"); err == nil && os.Getenv("TEAM_MEMORY_NO_CLAUDE_CLI") == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, claude, "mcp", "remove", "--scope", "user", "team-memory").Run()
		var stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, claude, "mcp", "add-json", "--scope", "user", "team-memory", string(entry))
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil {
			return "registered MCP server via `claude mcp add-json`", nil
		}
	}
	o, err := readObjectFile(p.ClaudeJSON)
	if err != nil {
		return "", err
	}
	servers, err := o.child("mcpServers")
	if err != nil {
		return "", err
	}
	servers.set("team-memory", entry)
	o.setChild("mcpServers", servers)
	return "registered MCP server in " + p.ClaudeJSON, writeObjectFile(p.ClaudeJSON, o)
}

// UnregisterMCP removes the MCP server registration.
func UnregisterMCP(p Paths) error {
	if claude, err := exec.LookPath("claude"); err == nil && os.Getenv("TEAM_MEMORY_NO_CLAUDE_CLI") == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, claude, "mcp", "remove", "--scope", "user", "team-memory").Run()
	}
	o, err := readObjectFile(p.ClaudeJSON)
	if err != nil {
		return err
	}
	servers, err := o.child("mcpServers")
	if err != nil {
		return err
	}
	if _, ok := servers.get("team-memory"); !ok {
		return nil
	}
	servers.del("team-memory")
	o.setChild("mcpServers", servers)
	return writeObjectFile(p.ClaudeJSON, o)
}

// MCPRegistered reports whether ~/.claude.json has our server.
func MCPRegistered(p Paths) bool {
	o, err := readObjectFile(p.ClaudeJSON)
	if err != nil {
		return false
	}
	servers, err := o.child("mcpServers")
	if err != nil {
		return false
	}
	_, ok := servers.get("team-memory")
	return ok
}

// Commands are the slash commands we install.
var Commands = map[string]string{
	"memory-search.md": `---
description: Search your team's shared memory
argument-hint: <what to look for>
---
Search team memory for: $ARGUMENTS

Use the team-memory ` + "`recall`" + ` tool with that query. Present the results grouped by system, with each memory's title, summary and where it was learned. If nothing is found, say so in one line.
`,
	"memory-add.md": `---
description: Save a note to your team's shared memory
argument-hint: <what to remember>
---
Save this to team memory: $ARGUMENTS

Use the team-memory ` + "`remember`" + ` tool with that text as the content (add ` + "`about`" + ` if the systems involved are obvious from our conversation). Reply with one line confirming the title it was saved under. Never include secrets.
`,
	"memory-review.md": `---
description: Review automatically captured memories waiting for your approval
---
Help me review team memories waiting for my approval. $ARGUMENTS

1. Call the team-memory ` + "`list_pending`" + ` tool.
2. If nothing is waiting, say so in one line and stop.
3. Otherwise list each item compactly — number, title, one-line summary, and why it was held — then ask what to do. I can answer in plain language ("approve all", "approve 1 and 3, reject 2", "approve 2 but title it …").
4. Apply my decisions with the ` + "`review`" + ` tool (action approve or reject, passing edits when I asked for changes) and confirm what was published.
`,
	"memory-mode.md": `---
description: Show or change how automatically captured memories are shared
argument-hint: [auto | ai | review-timeout | review | manual]
---
$ARGUMENTS

Use the team-memory ` + "`capture_mode`" + ` tool. If I named a mode above, set it (pass it as ` + "`mode`" + `); otherwise just show the current mode and the options with one line each. Confirm the result in one or two lines.
`,
}

// InstallCommands writes the slash commands.
func InstallCommands(p Paths) error {
	if err := os.MkdirAll(p.Commands, 0o755); err != nil {
		return err
	}
	for name, body := range Commands {
		if err := os.WriteFile(filepath.Join(p.Commands, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// RemoveCommands deletes the slash commands we installed.
func RemoveCommands(p Paths) {
	for name := range Commands {
		os.Remove(filepath.Join(p.Commands, name))
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
