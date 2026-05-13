package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/config"
	"github.com/AndrewSkea/team-memory/mcp/prompts"
)

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/v1/config", s.handleConfig)
	s.mux.HandleFunc("/v1/categorize", s.handleCategorize)
	s.mux.HandleFunc("/v1/summarize", s.handleSummarize)
	s.mux.HandleFunc("/v1/export-config", s.handleExportConfig)
	s.mux.HandleFunc("/v1/reminder", s.handleReminder)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	claude := "missing"
	if s.claudeAvailable() {
		claude = "available"
	}
	v := s.cfg.Version
	if v == "" {
		v = "dev"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "claude": claude, "version": v})
}

type categorizePayload struct {
	Scope     string `json:"scope"`
	Type      string `json:"type"`
	Text      string `json:"text"`
	Source    string `json:"source"`
	Timestamp string `json:"timestamp"`
}

type categorizeReq struct {
	Index       string            `json:"index"`
	Payload     categorizePayload `json:"payload"`
	TokenBudget int               `json:"token_budget"`
}

func (s *Server) handleCategorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req categorizeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	payloadJSON, _ := json.Marshal(req.Payload)
	prompt := prompts.Categorize +
		"\n\nINDEX:\n" + req.Index +
		"\nPAYLOAD:\n" + string(payloadJSON)

	if s.cfg.Runner == nil {
		writeErr(w, http.StatusServiceUnavailable, "no LLM runner configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	out, err := s.cfg.Runner.Run(ctx, prompt)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, stripFences(out))
}

type summarizeReq struct {
	Filename string `json:"filename"`
	Text     string `json:"text"`
}

func (s *Server) handleSummarize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req summarizeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	payloadJSON, _ := json.Marshal(req)
	prompt := prompts.Summarize + "\n\nINPUT:\n" + string(payloadJSON)

	if s.cfg.Runner == nil {
		writeErr(w, http.StatusServiceUnavailable, "no LLM runner configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	out, err := s.cfg.Runner.Run(ctx, prompt)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, stripFences(out))
}

type reminderReq struct {
	Title   string `json:"title"`
	DueDate string `json:"due_date"`
	Details string `json:"details"`
}

func (s *Server) handleReminder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req reminderReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	payloadJSON, _ := json.Marshal(req)
	prompt := prompts.Reminder + "\n\nINPUT:\n" + string(payloadJSON)
	if s.cfg.Runner == nil {
		writeErr(w, http.StatusServiceUnavailable, "no LLM runner configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	out, err := s.cfg.Runner.Run(ctx, prompt)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, stripFences(out))
}

// stripFences removes markdown code fences (```json ... ``` or ``` ... ```) that
// some Claude versions add around JSON output.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// drop first line (``` or ```json)
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	return strings.TrimSpace(s)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	path := s.cfg.ConfigPath
	if path == "" {
		path = config.DefaultPath()
	}
	switch r.Method {
	case http.MethodGet:
		cfg, err := config.Read(path)
		if err != nil {
			if config.IsMissing(err) {
				writeErr(w, http.StatusNotFound, "no config file")
				return
			}
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "GET only")
	}
}

func (s *Server) handleExportConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	path := s.cfg.ConfigPath
	if path == "" {
		path = config.DefaultPath()
	}
	if err := config.Write(path, cfg); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": path})
}
