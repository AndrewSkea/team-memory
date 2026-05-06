package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/AndrewSkea/team-memory/mcp/config"
	gh "github.com/AndrewSkea/team-memory/mcp/github"
	"github.com/AndrewSkea/team-memory/mcp/hook"
	"github.com/AndrewSkea/team-memory/mcp/llm"
	"github.com/AndrewSkea/team-memory/mcp/server"
)

func main() {
	once := flag.String("once", "", "run one-shot command (session-end|precompact) and exit")
	port := flag.String("port", "7438", "loopback port")
	flag.Parse()

	if *once != "" {
		if err := runOnce(*once); err != nil {
			fmt.Fprintf(os.Stderr, "team-memory: %v\n", err)
			os.Exit(1)
		}
		return
	}

	runner := llm.NewClaude("")
	srv := server.New(server.Config{Runner: runner})
	addr := "127.0.0.1:" + *port
	log.Printf("team-memory-mcp listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, server.WithCORS(srv.Handler())))
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
