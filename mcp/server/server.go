package server

import (
	"context"
	"net/http"
	"os/exec"

	gh "github.com/AndrewSkea/team-memory/mcp/github"
)

type LLMRunner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

type GitHubClient interface {
	GetFile(ctx context.Context, path string) (gh.FileInfo, error)
	CommitFile(ctx context.Context, path, appendContent, message string) error
}

type Config struct {
	ClaudePath   string
	Runner       LLMRunner
	ConfigPath   string
	Version      string
	GitHubClient GitHubClient // optional; if nil, quick-add reads config per-request
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
