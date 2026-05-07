package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/server"
)

func newTestHandler() http.Handler {
	srv := server.New(server.Config{ClaudePath: "/nonexistent/claude"})
	return buildHandler(srv)
}

// ── frontend serving ──────────────────────────────────────────────────────────

func TestFrontendIndex(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Team Memory") {
		t.Errorf("index.html missing 'Team Memory': %s", body[:min(200, len(body))])
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html*", ct)
	}
}

func TestFrontendAppJS(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/src/app.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /src/app.js = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "renderRemember") {
		t.Errorf("app.js missing expected content")
	}
}

func TestFrontendCSS(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/src/ui/main.css", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /src/ui/main.css = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "--bg") {
		t.Errorf("main.css missing expected CSS variables")
	}
}

func TestFrontendPages(t *testing.T) {
	pages := []string{
		"/src/pages/remember.js",
		"/src/pages/lookup.js",
		"/src/pages/stats.js",
		"/src/pages/stale.js",
		"/src/pages/setup.js",
	}
	h := newTestHandler()
	for _, p := range pages {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("GET %s = %d, want 200", p, w.Code)
			}
		})
	}
}

func TestFrontend404(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/nonexistent.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /nonexistent.js = %d, want 404", w.Code)
	}
}

// ── API routes not swallowed by frontend handler ───────────────────────────────

func TestAPIHealthViaMainHandler(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/health = %d, want 200", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
}

func TestAPIRouteViaMainHandler(t *testing.T) {
	// POST to /v1/categorize with bad JSON should return 400, not serve index.html
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/categorize", strings.NewReader("bad"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST /v1/categorize with bad JSON = %d, want 400", w.Code)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
