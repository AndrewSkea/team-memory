package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/AndrewSkea/team-memory/mcp/config"
	gh "github.com/AndrewSkea/team-memory/mcp/github"
	"github.com/AndrewSkea/team-memory/mcp/hook"
	"github.com/AndrewSkea/team-memory/mcp/llm"
	"github.com/AndrewSkea/team-memory/mcp/server"
)

var version = "dev"

func main() {
	once := flag.String("once", "", "run one-shot command (session-end|precompact) and exit")
	mcp  := flag.Bool("mcp", false, "run as MCP stdio server (used by Claude Code)")
	port := flag.String("port", "7438", "loopback port for HTTP server")
	flag.Parse()

	if *once != "" {
		if err := runOnce(*once); err != nil {
			fmt.Fprintf(os.Stderr, "team-memory: %v\n", err)
			os.Exit(1)
		}
		return
	}

	runner := llm.NewClaude("")
	srv := server.New(server.Config{Runner: runner, Version: version})
	handler := buildHandler(srv)
	addr := "127.0.0.1:" + *port

	if *mcp {
		go tryListen(addr, handler)
		go tryListen("127.0.0.1:80", handler)
		if err := runMCPStdio(); err != nil {
			log.Fatalf("MCP stdio: %v", err)
		}
		return
	}

	go tryListen("127.0.0.1:80", handler)
	log.Printf("team-memory listening on http://127.0.0.1:%s/  (also trying :80)", *port)
	log.Fatal(http.ListenAndServe(addr, handler))
}

func buildHandler(srv *server.Server) http.Handler {
	sub, err := fs.Sub(frontendFiles, "frontend")
	if err != nil {
		log.Fatalf("embed: %v", err)
	}
	fileServer := http.FileServer(http.FS(sub))
	apiHandler := server.WithCORS(srv.Handler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || strings.HasPrefix(r.URL.Path, "/v1/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func tryListen(addr string, h http.Handler) {
	if err := http.ListenAndServe(addr, h); err != nil {
		log.Printf("HTTP %s: %v", addr, err)
	}
}

type hookEvent struct {
	Transcript []hook.Message `json:"transcript"`
}

func runOnce(cmd string) error {
	if cmd != "session-end" && cmd != "precompact" {
		return fmt.Errorf("unknown --once command %q (want session-end|precompact)", cmd)
	}

	var event hookEvent
	if err := json.NewDecoder(os.Stdin).Decode(&event); err != nil {
		event.Transcript = nil
	}
	if len(event.Transcript) == 0 {
		return nil
	}

	cfg, err := config.Read(config.DefaultPath())
	if err != nil {
		if config.IsMissing(err) {
			return fmt.Errorf("no config found — open the web UI, complete Setup, click 'Export to CLI config'")
		}
		return err
	}

	client := gh.NewClient(cfg, "")
	runner := llm.NewClaude("")
	ctx := context.Background()
	adapter := &githubAdapter{c: client}

	switch cmd {
	case "session-end":
		return hook.RunSessionEnd(ctx, cfg, event.Transcript, runner, adapter)
	case "precompact":
		return hook.RunPreCompact(ctx, cfg, event.Transcript, runner, adapter)
	}
	return nil
}

type githubAdapter struct{ c *gh.Client }

func (a *githubAdapter) GetFile(ctx context.Context, path string) (hook.FileInfo, error) {
	f, err := a.c.GetFile(ctx, path)
	if err != nil {
		return hook.FileInfo{}, err
	}
	return hook.FileInfo{SHA: f.SHA, Content: f.Content, Exists: f.Exists}, nil
}

func (a *githubAdapter) CommitFile(ctx context.Context, path, appendContent, message string) error {
	return a.c.CommitFile(ctx, path, appendContent, message)
}

func (a *githubAdapter) PutContent(ctx context.Context, path, content, message string) error {
	return a.c.PutContent(ctx, path, content, message)
}
