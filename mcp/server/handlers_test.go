package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealth(t *testing.T) {
	srv := New(Config{ClaudePath: "/nonexistent/claude"})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want ok", body["status"])
	}
	if body["claude"] != "missing" {
		t.Errorf("claude field = %q, want missing", body["claude"])
	}
}

type fakeRunner struct {
	out string
	err error
	got string
}

func (f *fakeRunner) Run(_ context.Context, prompt string) (string, error) {
	f.got = prompt
	return f.out, f.err
}

func TestCategorize(t *testing.T) {
	runner := &fakeRunner{out: `{"target_file":"GENERAL.md","short_title":"x","one_sentence_summary":"y","bullets":[],"tags":"","entry_header":"Entry: 2026-05-02T00:00:00Z — x","unsure":false}`}
	srv := New(Config{Runner: runner})
	body := `{"index":"INDEX","payload":{"scope":"Team","type":"General","text":"hello","source":"UI","timestamp":"2026-05-02T00:00:00Z"},"token_budget":200}`
	req := httptest.NewRequest(http.MethodPost, "/v1/categorize", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"target_file":"GENERAL.md"`) {
		t.Errorf("missing target_file in response: %s", w.Body.String())
	}
	if !strings.Contains(runner.got, "INDEX") {
		t.Errorf("prompt did not contain INDEX content")
	}
}

func TestCategorize_BadJSON(t *testing.T) {
	srv := New(Config{Runner: &fakeRunner{}})
	req := httptest.NewRequest(http.MethodPost, "/v1/categorize", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
