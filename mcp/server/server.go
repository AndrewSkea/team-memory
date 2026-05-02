package server

import (
	"net/http"
	"os/exec"
)

type Config struct {
	ClaudePath string // override for tests; defaults to "claude" on PATH
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
