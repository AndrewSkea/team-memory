package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/prompts"
)

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/v1/categorize", s.handleCategorize)
	s.mux.HandleFunc("/v1/summarize", s.handleSummarize)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	claude := "missing"
	if s.claudeAvailable() {
		claude = "available"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "claude": claude})
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
	_, _ = fmt.Fprint(w, out)
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
	_, _ = fmt.Fprint(w, out)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
