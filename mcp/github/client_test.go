package github_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/config"
	gh "github.com/AndrewSkea/team-memory/mcp/github"
)

func newCfg() config.Config {
	return config.Config{Token: "tok", Owner: "o", Repo: "r"}
}

func TestGetFile404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	c := gh.NewClient(newCfg(), srv.URL)
	f, err := c.GetFile(context.Background(), "INDEX.md")
	if err != nil {
		t.Fatal(err)
	}
	if f.Exists {
		t.Fatal("expected exists=false")
	}
}

func TestGetFileSuccess(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte("hello"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"sha": "abc", "content": body + "\n"})
	}))
	defer srv.Close()

	c := gh.NewClient(newCfg(), srv.URL)
	f, err := c.GetFile(context.Background(), "INDEX.md")
	if err != nil {
		t.Fatal(err)
	}
	if !f.Exists || f.SHA != "abc" || f.Content != "hello" {
		t.Fatalf("unexpected: %+v", f)
	}
}

func TestCommitFileRetryOn409(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			body := base64.StdEncoding.EncodeToString([]byte("existing\n"))
			json.NewEncoder(w).Encode(map[string]string{"sha": "stale", "content": body})
			return
		}
		calls++
		if calls < 3 {
			w.WriteHeader(409)
			return
		}
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	c := gh.NewClient(newCfg(), srv.URL)
	err := c.CommitFile(context.Background(), "f.md", "\nappend", "msg")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 PUT calls, got %d", calls)
	}
}

func TestPutContentSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	c := gh.NewClient(newCfg(), srv.URL)
	err := c.PutContent(context.Background(), "new.md", "# New", "create")
	if err != nil {
		t.Fatal(err)
	}
}

func TestCommitFile409ThreeTimes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			body := base64.StdEncoding.EncodeToString([]byte("x"))
			json.NewEncoder(w).Encode(map[string]string{"sha": "s", "content": body})
			return
		}
		w.WriteHeader(409)
	}))
	defer srv.Close()

	c := gh.NewClient(newCfg(), srv.URL)
	err := c.CommitFile(context.Background(), "f.md", "data", "msg")
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict error, got %v", err)
	}
}
