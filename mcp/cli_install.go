package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/AndrewSkea/team-memory/mcp/app"
	"github.com/AndrewSkea/team-memory/mcp/config"
	"github.com/AndrewSkea/team-memory/mcp/installer"
	"github.com/AndrewSkea/team-memory/mcp/policy"
)

// tty is where interactive prompts read from. When installed via
// `curl … | sh`, stdin is the script, so read the terminal directly.
type tty struct {
	in  *os.File
	r   *bufio.Reader
	out io.Writer
}

func openTTY() *tty {
	t := &tty{in: os.Stdin, out: os.Stderr}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		name := "/dev/tty"
		if runtime.GOOS == "windows" {
			name = "CONIN$"
		}
		if f, err := os.Open(name); err == nil {
			t.in = f
		}
	}
	t.r = bufio.NewReader(t.in)
	return t
}

func (t *tty) interactive() bool { return term.IsTerminal(int(t.in.Fd())) }

func (t *tty) ask(prompt, def string) string {
	if def != "" {
		fmt.Fprintf(t.out, "%s [%s]: ", prompt, def)
	} else {
		fmt.Fprintf(t.out, "%s: ", prompt)
	}
	line, _ := t.r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func (t *tty) yes(prompt string, def bool) bool {
	d := "Y/n"
	if !def {
		d = "y/N"
	}
	fmt.Fprintf(t.out, "%s [%s]: ", prompt, d)
	line, _ := t.r.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return def
	case "y", "yes":
		return true
	}
	return false
}

func (t *tty) secret(prompt string) string {
	fmt.Fprintf(t.out, "%s: ", prompt)
	if t.interactive() {
		b, err := term.ReadPassword(int(t.in.Fd()))
		fmt.Fprintln(t.out)
		if err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	line, _ := t.r.ReadString('\n')
	return strings.TrimSpace(line)
}

func readAllStdin() (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return "", nil
	}
	b, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	return string(b), err
}

func binaryPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "team-memory-mcp"
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe
}

func cmdInstall(ctx context.Context, args []string) int {
	fset := flag.NewFlagSet("install", flag.ContinueOnError)
	repo := fset.String("repo", "", "memory repo, owner/name")
	token := fset.String("token", "", "GitHub token (or set GITHUB_TOKEN); prefer --use-gh or the prompt")
	useGH := fset.Bool("use-gh", false, "use the GitHub CLI's login instead of storing a token")
	mode := fset.String("mode", "", "capture mode: auto|ai|review-timeout|review|manual (default: team default)")
	service := fset.Bool("service", false, "keep the web UI running in the background (launchd/systemd)")
	noService := fset.Bool("no-service", false, "don't ask about the background service")
	yes := fset.Bool("yes", false, "non-interactive: accept defaults, fail instead of prompting")
	port := fset.Int("port", 7438, "web UI port for the background service")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if *token == "" {
		*token = os.Getenv("GITHUB_TOKEN")
	}
	t := openTTY()
	interactive := !*yes && t.interactive()
	out := os.Stderr

	a, err := openApp()
	if err != nil {
		fmt.Fprintf(out, "team-memory: %v\n", err)
		return 1
	}
	bin := binaryPath()
	fmt.Fprintf(out, "\nteam-memory %s\n\n", version)

	// ── 1. memory repo + access ─────────────────────────────────────────────
	cur := a.Config()
	needSetup := !cur.Configured() || *repo != "" || *token != "" || *useGH
	if cur.Configured() && interactive && !needSetup {
		fmt.Fprintf(out, "Memory repo: %s (as %s)\n", cur.Slug(), cur.Username)
		needSetup = !t.yes("Keep using it?", true)
	}
	if needSetup {
		ok := false
		for attempt := 0; attempt < 3 && !ok; attempt++ {
			in := app.SetupInput{Repo: *repo, Token: *token, UseGH: *useGH, Mode: *mode}
			if in.Repo == "" {
				def := ""
				if cur.Configured() {
					def = cur.Slug()
				}
				if !interactive {
					fmt.Fprintln(out, "error: --repo owner/name is required (non-interactive)")
					return 2
				}
				fmt.Fprintln(out, "Team memory lives in a GitHub repo you own (private is fine; create an empty one if needed).")
				in.Repo = t.ask("Memory repo (owner/name)", def)
			}
			if in.Token == "" && !in.UseGH {
				if cur.Configured() && in.Repo == cur.Slug() && attempt == 0 {
					in.KeepToken = true
				} else if _, err := config.GHToken(); err == nil && (!interactive || t.yes("Use your GitHub CLI (gh) login for access? (no token to paste or rotate)", true)) {
					in.UseGH = true
				} else if interactive {
					fmt.Fprintln(out, "Create a fine-grained token with Contents: Read and write on that repo:")
					fmt.Fprintln(out, "  https://github.com/settings/personal-access-tokens/new")
					in.Token = t.secret("GitHub token (input hidden)")
				} else {
					fmt.Fprintln(out, "error: provide --token, GITHUB_TOKEN or --use-gh (non-interactive)")
					return 2
				}
			}
			if in.Mode == "" && interactive && !cur.Configured() {
				in.Mode = askMode(t)
			}
			sctx, cancel := context.WithTimeout(ctx, 90*time.Second)
			res, err := a.Setup(sctx, in)
			cancel()
			if err != nil {
				fmt.Fprintf(out, "✗ %v\n\n", err)
				if !interactive {
					return 1
				}
				*repo, *token, *useGH = "", "", false
				continue
			}
			ok = true
			fmt.Fprintf(out, "✓ Connected to %s as %s — %d memories\n", res.Repo, res.Username, res.Memories)
			if len(res.Seeded) > 0 {
				fmt.Fprintf(out, "  initialised %s\n", strings.Join(res.Seeded, ", "))
			}
		}
		if !ok {
			return 1
		}
	} else if *mode != "" {
		if _, err := a.SetMode(*mode); err != nil {
			fmt.Fprintf(out, "! %v\n", err)
		}
	}

	// ── 2. Claude Code integration ──────────────────────────────────────────
	p := installer.DefaultPaths(bin)
	if _, err := installer.InstallHooks(p); err != nil {
		fmt.Fprintf(out, "✗ hooks: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "✓ Claude Code hooks installed (%s)\n", p.Settings)
	if msg, err := installer.RegisterMCP(p); err != nil {
		fmt.Fprintf(out, "✗ MCP server: %v\n", err)
		return 1
	} else {
		fmt.Fprintf(out, "✓ %s\n", msg)
	}
	if err := installer.InstallCommands(p); err != nil {
		fmt.Fprintf(out, "✗ slash commands: %v\n", err)
	} else {
		fmt.Fprintln(out, "✓ slash commands: /memory-search /memory-add /memory-review /memory-mode")
	}

	// ── 3. optional background service ──────────────────────────────────────
	wantService := *service
	if !wantService && !*noService && interactive && runtime.GOOS != "windows" && !installer.ServiceInstalled(p) {
		fmt.Fprintln(out, "\nThe web UI is available whenever Claude Code is running.")
		wantService = t.yes("Also keep it running in the background (starts at login)?", false)
	}
	if wantService || installer.ServiceInstalled(p) {
		if msg, err := installer.InstallService(p, *port, filepath.Join(a.DataDir, "logs")); err != nil {
			fmt.Fprintf(out, "! background service: %v\n", err)
		} else {
			fmt.Fprintf(out, "✓ %s\n", msg)
		}
	}

	s := a.Settings()
	fmt.Fprintf(out, "\nDone. Capture mode: %s — %s\n", s.EffectiveMode, s.ModeHelp[s.EffectiveMode])
	fmt.Fprintf(out, "Web UI: http://127.0.0.1:%s/   Check health any time: team-memory-mcp doctor\n", strconv.Itoa(*port))
	fmt.Fprintln(out, "Restart any running Claude Code sessions to pick up the hooks.")
	return 0
}

func askMode(t *tty) string {
	fmt.Fprintln(t.out, "\nHow should knowledge captured from your sessions be shared?")
	for i, m := range policy.Modes {
		def := ""
		if m == policy.AI {
			def = "  (recommended)"
		}
		fmt.Fprintf(t.out, "  %d) %-15s %s%s\n", i+1, m, policy.Descriptions[m], def)
	}
	ans := t.ask("Choice", "2")
	if n, err := strconv.Atoi(ans); err == nil && n >= 1 && n <= len(policy.Modes) {
		return string(policy.Modes[n-1])
	}
	if m, err := policy.ParseMode(ans); err == nil {
		return string(m)
	}
	return string(policy.AI)
}

func cmdUninstall(args []string) int {
	fset := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	purge := fset.Bool("purge", false, "also delete config (including token) and local data")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	p := installer.DefaultPaths(binaryPath())
	if _, err := installer.RemoveHooks(p); err != nil {
		fmt.Fprintf(os.Stderr, "hooks: %v\n", err)
	} else {
		fmt.Println("✓ removed Claude Code hooks")
	}
	if err := installer.UnregisterMCP(p); err != nil {
		fmt.Fprintf(os.Stderr, "MCP: %v\n", err)
	} else {
		fmt.Println("✓ removed MCP server")
	}
	installer.RemoveCommands(p)
	fmt.Println("✓ removed slash commands")
	if msg := installer.RemoveService(p); msg != "" {
		fmt.Println("✓ " + msg)
	}
	if *purge {
		cfg := config.DefaultPath()
		data := config.DataDir()
		if err := errors.Join(os.Remove(cfg), os.RemoveAll(data)); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "purge: %v\n", err)
		}
		fmt.Printf("✓ deleted %s and %s\n", cfg, data)
	}
	fmt.Println("Your memory repo on GitHub is untouched. Delete the binary to finish:", binaryPath())
	return 0
}
