package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOAuthGitHubStartRedirects(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodGet, "/oauth/github/start?client_id=test-client", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "github.com/login/oauth/authorize") {
		t.Errorf("redirect should go to GitHub, got: %s", loc)
	}
	if !strings.Contains(loc, "client_id=test-client") {
		t.Errorf("redirect missing client_id, got: %s", loc)
	}
	if !strings.Contains(loc, "state=") {
		t.Errorf("redirect missing state param, got: %s", loc)
	}
}

func TestOAuthGitHubStartMissingClientId(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodGet, "/oauth/github/start", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestOAuthGitHubCallbackBadState(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodGet, "/oauth/github/callback?code=abc&state=invalid", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "gh_error=") {
		t.Errorf("redirect missing gh_error, got: %s", loc)
	}
}

func TestOAuthSlackStartRedirects(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodGet, "/oauth/slack/start?client_id=slack-client", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "slack.com/oauth/v2/authorize") {
		t.Errorf("redirect should go to Slack, got: %s", loc)
	}
}

func TestOAuthSlackCallbackBadState(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodGet, "/oauth/slack/callback?code=abc&state=invalid", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "slack_error=") {
		t.Errorf("redirect missing slack_error, got: %s", loc)
	}
}

func TestOAuthStateNonceExpiry(t *testing.T) {
	store := newNonceStore()
	store.set("mystate", "test-client")
	cid, ok := store.consume("mystate")
	if !ok {
		t.Error("valid state should be accepted")
	}
	if cid != "test-client" {
		t.Errorf("clientID = %q, want test-client", cid)
	}
	_, ok2 := store.consume("mystate")
	if ok2 {
		t.Error("state should only be consumed once")
	}
	_, ok3 := store.consume("nonexistent")
	if ok3 {
		t.Error("nonexistent state should be rejected")
	}
}
