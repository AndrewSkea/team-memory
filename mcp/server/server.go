// Package server is the local HTTP API behind the web UI. It binds to
// loopback only, accepts same-origin browser requests only, and never
// returns the GitHub token.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/app"
	"github.com/AndrewSkea/team-memory/mcp/entities"
	gh "github.com/AndrewSkea/team-memory/mcp/github"
	"github.com/AndrewSkea/team-memory/mcp/search"
)

type Server struct {
	App     *app.App
	Version string
	mux     *http.ServeMux
}

// New builds the API. frontend (may be nil) serves the web UI.
func New(a *app.App, version string, frontend fs.FS) *Server {
	s := &Server{App: a, Version: version, mux: http.NewServeMux()}
	s.routes()
	if frontend != nil {
		s.mux.Handle("GET /", http.FileServer(http.FS(frontend)))
	}
	return s
}

// Handler returns the full handler with security middleware applied.
func (s *Server) Handler() http.Handler {
	return securityHeaders(WithCORS(s.mux))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.health)
	s.mux.HandleFunc("GET /v1/settings", s.getSettings)
	s.mux.HandleFunc("PUT /v1/settings", s.putSettings)
	s.mux.HandleFunc("POST /v1/setup", s.setup)
	s.mux.HandleFunc("POST /v1/sync", s.sync)
	s.mux.HandleFunc("GET /v1/memories", s.listMemories)
	s.mux.HandleFunc("POST /v1/memories", s.createMemory)
	s.mux.HandleFunc("GET /v1/memories/{id}", s.getMemory)
	s.mux.HandleFunc("POST /v1/memories/{id}/forget", s.forget)
	s.mux.HandleFunc("POST /v1/memories/{id}/supersede", s.supersede)
	s.mux.HandleFunc("GET /v1/pending", s.listPending)
	s.mux.HandleFunc("POST /v1/pending/{id}/approve", s.approve)
	s.mux.HandleFunc("POST /v1/pending/{id}/reject", s.reject)
	s.mux.HandleFunc("GET /v1/stats", s.stats)
	s.mux.HandleFunc("GET /v1/entities", s.entities)
	// Back-compat for pre-1.0 /memory-add slash commands.
	s.mux.HandleFunc("POST /v1/quick-add", s.quickAdd)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// fail maps app errors to HTTP statuses.
func fail(w http.ResponseWriter, err error) {
	var se *gh.StatusError
	switch {
	case errors.Is(err, app.ErrNotConfigured):
		writeErr(w, http.StatusServiceUnavailable, err.Error())
	case errors.As(err, &se):
		writeErr(w, http.StatusBadGateway, err.Error())
	case strings.HasPrefix(err.Error(), "no memory with id"), strings.HasPrefix(err.Error(), "no pending item"):
		writeErr(w, http.StatusNotFound, err.Error())
	default:
		writeErr(w, http.StatusBadRequest, err.Error())
	}
}

// decode reads a JSON body. Requiring application/json blocks cross-site
// "simple" form posts (CSRF), since browsers must preflight it.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "application/json" {
		writeErr(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func ctx(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	st := s.App.Settings()
	v := s.Version
	if v == "" {
		v = "dev"
	}
	writeJSON(w, 200, map[string]any{"status": "ok", "version": v, "configured": st.Configured, "llm": st.LLM,
		// kept for older clients that checked this field
		"claude": map[bool]string{true: "available", false: "missing"}[st.LLM == "claude-cli"]})
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.Settings())
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode *string `json:"mode"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Mode != nil {
		if _, err := s.App.SetMode(*in.Mode); err != nil {
			// Clamped modes still apply; report the clamp as a warning.
			writeJSON(w, 200, map[string]any{"settings": s.App.Settings(), "warning": err.Error()})
			return
		}
	}
	writeJSON(w, 200, map[string]any{"settings": s.App.Settings()})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var in app.SetupInput
	if !decode(w, r, &in) {
		return
	}
	c, cancel := ctx(r, 60*time.Second)
	defer cancel()
	res, err := s.App.Setup(c, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) sync(w http.ResponseWriter, r *http.Request) {
	c, cancel := ctx(r, 60*time.Second)
	defer cancel()
	changed, err := s.App.Sync(c)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"changed": changed, "state": s.App.SyncState()})
}

func (s *Server) listMemories(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var about []string
	if a := strings.TrimSpace(q.Get("about")); a != "" {
		about = strings.Split(a, ",")
	}
	c, cancel := ctx(r, 10*time.Second)
	defer cancel()
	s.App.EnsureFresh(c, 2*time.Minute)
	rs, err := s.App.Search(app.SearchInput{Query: q.Get("q"), About: about, Type: q.Get("type"), Limit: limit, IncludeAll: q.Get("all") == "1"})
	if err != nil {
		fail(w, err)
		return
	}
	if rs == nil {
		rs = []search.Result{}
	}
	writeJSON(w, 200, map[string]any{"results": rs})
}

func (s *Server) getMemory(w http.ResponseWriter, r *http.Request) {
	m, err := s.App.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, m)
}

func (s *Server) createMemory(w http.ResponseWriter, r *http.Request) {
	var in app.RememberInput
	if !decode(w, r, &in) {
		return
	}
	if in.Source == "" {
		in.Source = "ui"
	}
	c, cancel := ctx(r, 2*time.Minute)
	defer cancel()
	m, warns, err := s.App.Remember(c, in)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"memory": m, "warnings": warns})
}

func (s *Server) forget(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &in) {
		return
	}
	c, cancel := ctx(r, time.Minute)
	defer cancel()
	m, err := s.App.Forget(c, r.PathValue("id"), in.Reason)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, m)
}

func (s *Server) supersede(w http.ResponseWriter, r *http.Request) {
	var in app.RememberInput
	if !decode(w, r, &in) {
		return
	}
	if in.Source == "" {
		in.Source = "ui"
	}
	c, cancel := ctx(r, 2*time.Minute)
	defer cancel()
	m, warns, err := s.App.Supersede(c, r.PathValue("id"), in)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"memory": m, "warnings": warns})
}

func (s *Server) listPending(w http.ResponseWriter, r *http.Request) {
	items, err := s.App.ListPending()
	if err != nil {
		fail(w, err)
		return
	}
	if items == nil {
		items = []app.PendingItem{}
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	var e app.Edits
	if !decode(w, r, &e) {
		return
	}
	c, cancel := ctx(r, time.Minute)
	defer cancel()
	m, err := s.App.Approve(c, r.PathValue("id"), &e)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, m)
}

func (s *Server) reject(w http.ResponseWriter, r *http.Request) {
	var ignored struct{}
	if !decode(w, r, &ignored) {
		return
	}
	if err := s.App.Reject(r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	st, err := s.App.Stats()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) entities(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.View()
	if err != nil {
		fail(w, err)
		return
	}
	ents := v.Snap.Entities.Entities
	if ents == nil {
		ents = []entities.Entity{}
	}
	writeJSON(w, 200, map[string]any{"entities": ents})
}

func (s *Server) quickAdd(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text  string `json:"text"`
		Title string `json:"title"`
	}
	if !decode(w, r, &in) {
		return
	}
	c, cancel := ctx(r, 2*time.Minute)
	defer cancel()
	m, warns, err := s.App.Remember(c, app.RememberInput{Text: in.Text, Title: in.Title, Source: "cli", Classify: true})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "file": m.Path, "id": m.ID, "title": m.Title, "warnings": warns})
}

// securityHeaders applies a strict CSP: the UI only talks to this server.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'none'")
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// Background keeps a long-running server fresh: sync, retry queued captures
// and publish expired review-timeout items.
func Background(ctx context.Context, a *app.App, every time.Duration) {
	tick := func() {
		c, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		if !a.Configured() {
			return
		}
		if _, err := a.Sync(c); err != nil {
			log.Printf("background sync: %v", err)
		}
		a.ExpirePending(c)
		if a.QueueLen() > 0 {
			a.ProcessQueue(c)
		}
	}
	tick()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}
