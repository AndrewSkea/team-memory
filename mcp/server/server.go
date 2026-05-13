package server

import (
	"context"
	"net/http"
	"os/exec"
)

type LLMRunner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

type Config struct {
	ClaudePath string
	Runner     LLMRunner // overrideable for tests
	ConfigPath string    // path to config.json; empty = config.DefaultPath()
	Version    string    // injected by goreleaser ldflags; empty = "dev"
}

type Server struct {
	cfg Config
	mux *http.ServeMux
}

func New(cfg Config) *Server {
	if cfg.ClaudePath == "" {
		cfg.ClaudePath = "claude"
	}
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) claudeAvailable() bool {
	_, err := exec.LookPath(s.cfg.ClaudePath)
	return err == nil
}
