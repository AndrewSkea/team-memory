package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/config"
	gh "github.com/AndrewSkea/team-memory/mcp/github"
	"github.com/AndrewSkea/team-memory/mcp/hook"
	"github.com/AndrewSkea/team-memory/mcp/prompts"
)

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/v1/config", s.handleConfig)
	s.mux.HandleFunc("/v1/categorize", s.handleCategorize)
	s.mux.HandleFunc("/v1/summarize", s.handleSummarize)
	s.mux.HandleFunc("/v1/export-config", s.handleExportConfig)
	s.mux.HandleFunc("/v1/reminder", s.handleReminder)
	s.mux.HandleFunc("/v1/quick-add", s.handleQuickAdd)
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
	payloadJSON, err := json.Marshal(req.Payload)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "marshal payload: "+err.Error())
		return
	}
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
		log.Printf("categorize: runner failed: %v", err)
		writeErr(w, http.StatusBadGateway, "LLM runner failed")
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
	payloadJSON, err := json.Marshal(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "marshal payload: "+err.Error())
		return
	}
	prompt := prompts.Summarize + "\n\nINPUT:\n" + string(payloadJSON)

	if s.cfg.Runner == nil {
		writeErr(w, http.StatusServiceUnavailable, "no LLM runner configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	out, err := s.cfg.Runner.Run(ctx, prompt)
	if err != nil {
		log.Printf("summarize: runner failed: %v", err)
		writeErr(w, http.StatusBadGateway, "LLM runner failed")
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
	payloadJSON, err := json.Marshal(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "marshal payload: "+err.Error())
		return
	}
	prompt := prompts.Reminder + "\n\nINPUT:\n" + string(payloadJSON)
	if s.cfg.Runner == nil {
		writeErr(w, http.StatusServiceUnavailable, "no LLM runner configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	out, err := s.cfg.Runner.Run(ctx, prompt)
	if err != nil {
		log.Printf("reminder: runner failed: %v", err)
		writeErr(w, http.StatusBadGateway, "LLM runner failed")
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

type quickAddReq struct {
	Text  string `json:"text"`
	Title string `json:"title"`
}

func (s *Server) handleQuickAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var req quickAddReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeErr(w, http.StatusBadRequest, "text is required")
		return
	}

	client := s.cfg.GitHubClient
	if client == nil {
		cfgPath := s.cfg.ConfigPath
		if cfgPath == "" {
			cfgPath = config.DefaultPath()
		}
		cfg, err := config.Read(cfgPath)
		if err != nil {
			if config.IsMissing(err) {
				writeErr(w, http.StatusServiceUnavailable, "no config found — open the web UI, complete Setup, click 'Export to CLI config'")
				return
			}
			log.Printf("quick-add: config read: %v", err)
			writeErr(w, http.StatusInternalServerError, "could not read config")
			return
		}
		client = gh.NewClient(cfg, "")
	}

	if s.cfg.Runner == nil {
		writeErr(w, http.StatusServiceUnavailable, "no LLM runner configured")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Fetch INDEX.md so the LLM can pick the right file.
	idx, err := client.GetFile(ctx, "INDEX.md")
	if err != nil {
		log.Printf("quick-add: get INDEX.md: %v", err)
		writeErr(w, http.StatusBadGateway, "could not read INDEX.md from GitHub")
		return
	}
	indexContent := ""
	if idx.Exists {
		indexContent = idx.Content
	}

	title := req.Title
	if title == "" {
		t := strings.TrimSpace(req.Text)
		runes := []rune(t)
		if i := strings.IndexAny(t, ".!?\n"); i > 0 {
			// i is a byte offset; convert sentence to runes and cap at 60
			sentence := []rune(t[:i])
			if len(sentence) > 60 {
				sentence = sentence[:60]
			}
			title = string(sentence)
		} else if len(runes) > 60 {
			title = string(runes[:60])
		} else {
			title = t
		}
	}

	payload, _ := json.Marshal(map[string]string{
		"scope":     "Team",
		"type":      "General",
		"text":      req.Text,
		"source":    "CLI",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
	prompt := prompts.Categorize +
		"\n\nINDEX:\n" + indexContent +
		"\nPAYLOAD:\n" + string(payload)

	out, err := s.cfg.Runner.Run(ctx, prompt)
	if err != nil {
		log.Printf("quick-add: categorize runner: %v", err)
		writeErr(w, http.StatusBadGateway, "LLM runner failed")
		return
	}
	out = stripFences(out)

	type categorizeResult struct {
		TargetFile         string   `json:"target_file"`
		ShortTitle         string   `json:"short_title"`
		OneSentenceSummary string   `json:"one_sentence_summary"`
		Bullets            []string `json:"bullets"`
		Tags               string   `json:"tags"`
		Unsure             bool     `json:"unsure"`
	}
	var cat categorizeResult
	if err := json.Unmarshal([]byte(out), &cat); err != nil {
		log.Printf("quick-add: parse categorize output: %v (raw: %.300s)", err, out)
		// Fall back to GENERAL.md rather than failing entirely.
		cat = categorizeResult{TargetFile: "GENERAL.md", ShortTitle: title}
	}

	target := cat.TargetFile
	if cat.Unsure || target == "" || gh.ValidatePath(target) != nil {
		target = "GENERAL.md"
	}

	if cat.ShortTitle != "" {
		title = cat.ShortTitle
	}
	summary := cat.OneSentenceSummary
	if summary == "" {
		summary = req.Text
	}

	entry := hook.Entry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		ShortTitle: title,
		Scope:      "Team",
		Type:       "General",
		Tags:       cat.Tags,
		Source:     "CLI",
		Summary:    summary,
		Bullets:    cat.Bullets,
		Full:       req.Text,
	}
	block := "\n" + hook.RenderEntry(entry)

	if err := client.CommitFile(ctx, target, block, "team-memory: add entry to "+target); err != nil {
		log.Printf("quick-add: commit %s: %v", target, err)
		writeErr(w, http.StatusBadGateway, "could not commit to GitHub: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "file": target})
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
			log.Printf("config read: %v", err)
			writeErr(w, http.StatusInternalServerError, "could not read config")
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
		log.Printf("config write: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not write config")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": path})
}
