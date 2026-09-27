package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/installer"
)

func cmdDoctor(ctx context.Context) int {
	a := mustApp()
	failures := 0
	check := func(ok bool, good, bad string) {
		if ok {
			fmt.Printf("✓ %s\n", good)
		} else {
			fmt.Printf("✗ %s\n", bad)
			failures++
		}
	}
	warn := func(msg string) { fmt.Printf("! %s\n", msg) }

	fmt.Printf("team-memory %s (%s)\n\n", version, binaryPath())
	cfg := a.Config()
	check(cfg.Configured(), "config: "+a.CfgPath, "not set up — run `team-memory-mcp install` ("+a.CfgPath+")")
	if cfg.Configured() {
		c, _ := a.Client()
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		u, err := c.GetUser(cctx)
		check(err == nil, "GitHub access as "+u.Login+tokenNote(cfg.TokenSource), fmt.Sprintf("GitHub access: %v", err))
		if err == nil {
			r, err := c.GetRepo(cctx)
			check(err == nil && r.Permissions.Push, "memory repo "+r.FullName+" (writable)", fmt.Sprintf("memory repo %s: %v (push=%v)", cfg.Slug(), err, r.Permissions.Push))
		}
		cancel()
		sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err = a.Sync(sctx)
		cancel()
		check(err == nil, "synced local copy", fmt.Sprintf("sync: %v", err))
		if v, err := a.View(); err == nil {
			fmt.Printf("  %d memories, %d known systems\n", len(v.Snap.Memories), len(v.Snap.Entities.Entities))
			for _, p := range v.Snap.Problems {
				warn("unreadable file in memory repo: " + p)
			}
		}
	}
	s := a.Settings()
	check(s.LLM != "unavailable", "LLM for capture/classification: "+s.LLM, "no LLM: install Claude Code (`claude`) or set ANTHROPIC_API_KEY — capture is disabled")
	fmt.Printf("✓ capture mode: %s (team default %s, minimum %s)\n", s.EffectiveMode, s.TeamDefault, s.TeamMinimum)

	p := installer.DefaultPaths(binaryPath())
	hooks, err := installer.HooksInstalled(p)
	check(err == nil && len(hooks) == len(installer.Hooks), fmt.Sprintf("Claude Code hooks (%d/%d)", len(hooks), len(installer.Hooks)),
		fmt.Sprintf("Claude Code hooks %d/%d installed %v — run `team-memory-mcp install`", len(hooks), len(installer.Hooks), errString(err)))
	check(installer.MCPRegistered(p), "MCP server registered", "MCP server not found in ~/.claude.json — run `team-memory-mcp install`")
	if _, err := os.Stat(p.Commands + "/memory-review.md"); err != nil {
		warn("slash commands missing — run `team-memory-mcp install`")
	}
	if s.PendingCount > 0 {
		warn(fmt.Sprintf("%d memories waiting for your review (/memory-review or `team-memory-mcp pending`)", s.PendingCount))
	}
	if s.QueueLength > 0 {
		warn(fmt.Sprintf("%d capture jobs queued (will retry automatically)", s.QueueLength))
	}
	hc := &http.Client{Timeout: 2 * time.Second}
	if resp, err := hc.Get("http://127.0.0.1:7438/health"); err == nil {
		resp.Body.Close()
		fmt.Println("✓ web UI running at http://127.0.0.1:7438/")
	} else {
		fmt.Println("· web UI not running right now (starts with Claude Code, or run `team-memory-mcp serve`)")
	}
	fmt.Printf("  logs: %s/logs\n", a.DataDir)
	if failures > 0 {
		fmt.Printf("\n%d problem(s) found.\n", failures)
		return 1
	}
	fmt.Println("\nAll good.")
	return 0
}

func tokenNote(source string) string {
	if source == "gh" {
		return " (via GitHub CLI login)"
	}
	return ""
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return "(" + err.Error() + ")"
}
