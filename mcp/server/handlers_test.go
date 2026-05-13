package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestGetConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"token":"tok","owner":"o","repo":"r","check_first":false}`), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := New(Config{ConfigPath: cfgPath})
	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var cfg map[string]any
	if err := json.NewDecoder(w.Body).Decode(&cfg); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if cfg["token"] != "tok" || cfg["owner"] != "o" || cfg["repo"] != "r" {
		t.Errorf("unexpected config: %v", cfg)
	}
}

func TestGetConfig_Missing(t *testing.T) {
	srv := New(Config{ConfigPath: filepath.Join(t.TempDir(), "nonexistent.json")})
	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleReminder(t *testing.T) {
	runner := &fakeRunner{out: `{"short_title":"Submit report","bullets":["Review submissions"],"tags":"deadline"}`}
	srv := New(Config{Runner: runner})
	body := `{"title":"Submit report","due_date":"2026-05-20","details":"See email from boss"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/reminder", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out map[string]any
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if out["short_title"] != "Submit report" {
		t.Errorf("unexpected short_title: %v", out["short_title"])
	}
}

func TestExportConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	srv := New(Config{ClaudePath: "/nonexistent/claude", ConfigPath: cfgPath})
	body := `{"token":"tok","owner":"o","repo":"r","check_first":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/export-config", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["ok"] != true {
		t.Fatalf("expected ok:true, got %v", resp)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatal("config file not written")
	}
}
