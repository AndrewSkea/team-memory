package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/AndrewSkea/team-memory/mcp/app"
	"github.com/AndrewSkea/team-memory/mcp/hook"
	"github.com/AndrewSkea/team-memory/mcp/mcpserver"
	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/server"
)

var version = "dev"

const defaultPort = "7438"

const usage = `team-memory-mcp — shared, git-backed memory for your team's AI assistants

Usage:
  team-memory-mcp install [flags]     set up the memory repo and wire into Claude Code
  team-memory-mcp doctor              check that everything is working
  team-memory-mcp serve [--port N]    run the web UI (default http://127.0.0.1:7438)
  team-memory-mcp search <query>      search team memory
  team-memory-mcp add <text>          save a note to team memory
  team-memory-mcp pending             list memories waiting for your review
  team-memory-mcp approve <id>        publish a pending memory
  team-memory-mcp reject <id>         discard a pending memory
  team-memory-mcp mode [MODE]         show or set capture mode (auto|ai|review-timeout|review|manual)
  team-memory-mcp sync                refresh the local copy of the memory repo
  team-memory-mcp uninstall [--purge] remove hooks, MCP server, commands and service
  team-memory-mcp version

Used by Claude Code:
  team-memory-mcp mcp                 MCP stdio server
  team-memory-mcp hook <event>        hook handler (session-start|prompt-submit|precompact|session-end)
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(args) == 0 {
		return cmdServe(ctx, nil)
	}
	cmd, rest := args[0], args[1:]
	// Pre-1.0 flag-style invocations still found in users' settings files.
	if strings.HasPrefix(cmd, "-") {
		return legacy(ctx, args)
	}
	switch cmd {
	case "serve":
		return cmdServe(ctx, rest)
	case "mcp":
		return cmdMCP(ctx, rest)
	case "hook":
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "usage: team-memory-mcp hook <session-start|prompt-submit|precompact|session-end>")
			return 2
		}
		return cmdHook(ctx, rest[0])
	case "install":
		return cmdInstall(ctx, rest)
	case "uninstall":
		return cmdUninstall(rest)
	case "doctor":
		return cmdDoctor(ctx)
	case "search", "recall":
		return cmdSearch(ctx, rest)
	case "add", "remember":
		return cmdAdd(ctx, rest)
	case "pending":
		return cmdPending()
	case "approve":
		return cmdReview(ctx, rest, true)
	case "reject":
		return cmdReview(ctx, rest, false)
	case "mode":
		return cmdMode(rest)
	case "sync":
		return cmdSync(ctx)
	case "version", "--version":
		fmt.Println(version)
		return 0
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	}
	fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
	return 2
}

func legacy(ctx context.Context, args []string) int {
	fset := flag.NewFlagSet("team-memory-mcp", flag.ContinueOnError)
	once := fset.String("once", "", "legacy: session-end|precompact")
	mcp := fset.Bool("mcp", false, "legacy: MCP stdio server")
	port := fset.String("port", defaultPort, "HTTP port")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	switch {
	case *once != "":
		return cmdHook(ctx, *once)
	case *mcp:
		return cmdMCP(ctx, []string{"--port", *port})
	}
	return cmdServe(ctx, []string{"--port", *port})
}

func openApp() (*app.App, error) {
	return app.Open(app.Options{})
}

func mustApp() *app.App {
	a, err := openApp()
	if err != nil {
		fmt.Fprintf(os.Stderr, "team-memory: %v\n", err)
		os.Exit(1)
	}
	return a
}

func frontend() fs.FS {
	sub, err := fs.Sub(frontendFiles, "frontend")
	if err != nil {
		return nil
	}
	return sub
}

// listen binds the web UI port on loopback. It returns nil if the port is
// taken (another session or the background service already serves the UI).
func listen(port string) net.Listener {
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return nil
	}
	return ln
}

func serveHTTP(ctx context.Context, a *app.App, ln net.Listener) {
	srv := &http.Server{
		Handler:           server.New(a, version, frontend()).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	go server.Background(ctx, a, 5*time.Minute)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		a.Log.Printf("http: %v", err)
	}
}

func cmdServe(ctx context.Context, args []string) int {
	fset := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := fset.String("port", defaultPort, "loopback port for the web UI")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	a := mustApp()
	ln := listen(*port)
	if ln == nil {
		fmt.Fprintf(os.Stderr, "team-memory: port %s is in use — is team-memory already running? Try http://127.0.0.1:%s/ or use --port\n", *port, *port)
		return 1
	}
	fmt.Fprintf(os.Stderr, "team-memory %s: web UI at http://127.0.0.1:%s/\n", version, *port)
	if !a.Configured() {
		fmt.Fprintln(os.Stderr, "Not set up yet — open the web UI to finish setup, or run `team-memory-mcp install`.")
	}
	serveHTTP(ctx, a, ln)
	return 0
}

func cmdMCP(ctx context.Context, args []string) int {
	fset := flag.NewFlagSet("mcp", flag.ContinueOnError)
	port := fset.String("port", defaultPort, "also serve the web UI on this port if free")
	noHTTP := fset.Bool("no-http", false, "don't serve the web UI")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	a := mustApp()
	if !*noHTTP {
		if ln := listen(*port); ln != nil {
			go serveHTTP(ctx, a, ln)
		}
	}
	s := &mcpserver.Server{App: a, Version: version}
	if err := s.Serve(ctx, os.Stdin, os.Stdout); err != nil {
		a.Log.Printf("mcp: %v", err)
		return 1
	}
	return 0
}

func cmdHook(ctx context.Context, event string) int {
	if hook.Nested() {
		return 0
	}
	a, err := openApp()
	if err != nil {
		// Never break the user's Claude Code session over our problems.
		fmt.Fprintf(os.Stderr, "team-memory: %v\n", err)
		return 0
	}
	if err := hook.Run(ctx, a, event, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "team-memory: %v\n", err)
		return 2
	}
	return 0
}

func cmdSearch(ctx context.Context, args []string) int {
	q := strings.TrimSpace(strings.Join(args, " "))
	if q == "" {
		fmt.Fprintln(os.Stderr, "usage: team-memory-mcp search <query>")
		return 2
	}
	a := mustApp()
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	a.EnsureFresh(sctx, 2*time.Minute)
	cancel()
	rs, err := a.Search(app.SearchInput{Query: q, Limit: 10})
	if err != nil {
		fmt.Fprintf(os.Stderr, "team-memory: %v\n", err)
		return 1
	}
	if len(rs) == 0 {
		fmt.Println("No matching memories.")
		return 0
	}
	for _, r := range rs {
		printMemory(a, r.Memory)
	}
	return 0
}

func printMemory(a *app.App, m memory.Memory) {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Printf("\033[1m%s\033[0m  [%s]\n", m.Title, m.ID)
	} else {
		fmt.Printf("%s  [%s]\n", m.Title, m.ID)
	}
	var meta []string
	if m.Type != "" {
		meta = append(meta, m.Type)
	}
	if len(m.About) > 0 {
		meta = append(meta, "about "+strings.Join(m.About, ", "))
	}
	if m.LearnedIn != "" {
		meta = append(meta, "learned in "+m.LearnedIn)
	}
	if m.Author != "" {
		meta = append(meta, "by "+m.Author)
	}
	if !m.Created.IsZero() {
		meta = append(meta, app.Ago(a.Now(), m.Created))
	}
	fmt.Printf("  %s\n", strings.Join(meta, " · "))
	if m.Summary != "" {
		fmt.Printf("  %s\n", m.Summary)
	}
	fmt.Println()
}

func cmdAdd(ctx context.Context, args []string) int {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" || text == "-" {
		b, _ := readAllStdin()
		text = strings.TrimSpace(b)
	}
	if text == "" {
		fmt.Fprintln(os.Stderr, "usage: team-memory-mcp add <text>   (or pipe text on stdin)")
		return 2
	}
	a := mustApp()
	cwd, _ := os.Getwd()
	m, warns, err := a.Remember(ctx, app.RememberInput{Text: text, Source: "cli", Cwd: cwd, Classify: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "team-memory: %v\n", err)
		return 1
	}
	fmt.Printf("Saved: %s [%s]\n", m.Title, m.ID)
	for _, w := range warns {
		fmt.Printf("Note: %s\n", w)
	}
	return 0
}

func cmdPending() int {
	a := mustApp()
	items, err := a.ListPending()
	if err != nil {
		fmt.Fprintf(os.Stderr, "team-memory: %v\n", err)
		return 1
	}
	if len(items) == 0 {
		fmt.Println("Nothing is waiting for review.")
		return 0
	}
	for _, p := range items {
		printMemory(a, p.Memory)
		fmt.Printf("  held because: %s\n  approve: team-memory-mcp approve %s   reject: team-memory-mcp reject %s\n\n", p.Reason, p.ID(), p.ID())
	}
	return 0
}

func cmdReview(ctx context.Context, args []string, approve bool) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: team-memory-mcp approve|reject <id>... (or 'all')")
		return 2
	}
	a := mustApp()
	ids := args
	if len(args) == 1 && args[0] == "all" {
		items, _ := a.ListPending()
		ids = nil
		for _, p := range items {
			ids = append(ids, p.ID())
		}
	}
	code := 0
	for _, id := range ids {
		if approve {
			m, err := a.Approve(ctx, id, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", id, err)
				code = 1
				continue
			}
			fmt.Printf("Published: %s\n", m.Title)
		} else {
			if err := a.Reject(id); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", id, err)
				code = 1
				continue
			}
			fmt.Printf("Rejected: %s\n", id)
		}
	}
	return code
}

func cmdMode(args []string) int {
	a := mustApp()
	if len(args) > 0 {
		mode := args[0]
		if mode == "default" {
			mode = ""
		}
		eff, err := a.SetMode(mode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "team-memory: %v\n", err)
			if eff == "" {
				return 1
			}
		}
	}
	s := a.Settings()
	fmt.Printf("Capture mode: %s — %s\n", s.EffectiveMode, s.ModeHelp[s.EffectiveMode])
	fmt.Printf("Team default: %s, team minimum: %s\n\nModes you can choose:\n", s.TeamDefault, s.TeamMinimum)
	for _, m := range s.AllowedModes {
		fmt.Printf("  %-15s %s\n", m, s.ModeHelp[m])
	}
	return 0
}

func cmdSync(ctx context.Context) int {
	a := mustApp()
	changed, err := a.Sync(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "team-memory: %v\n", err)
		return 1
	}
	v, _ := a.View()
	n := 0
	if v != nil {
		n = len(v.Snap.Memories)
	}
	if changed {
		fmt.Printf("Updated — %d memories.\n", n)
	} else {
		fmt.Printf("Up to date — %d memories.\n", n)
	}
	a.ExpirePending(ctx)
	if a.QueueLen() > 0 {
		a.ProcessQueue(ctx)
	}
	return 0
}
