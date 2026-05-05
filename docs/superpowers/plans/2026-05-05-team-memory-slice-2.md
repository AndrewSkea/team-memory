# team-memory Slice 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make memory capture automatic — Claude Code Stop/PreCompact hooks call `team-memory-mcp --once session-end|precompact`, which reads the session transcript from stdin, summarizes it via claude CLI, and commits a structured entry to GitHub.

**Architecture:** New `--once` flag in main.go dispatches one-shot commands that read hook JSON from stdin, load config from disk, call claude for session summarization (reusing categorize JSON schema), build an entry block (Go port of `renderEntry`), and commit via GitHub REST. Config is written by a new `/v1/export-config` MCP endpoint triggered from the Setup page. Frontend gains a "check_first" toggle and "Export to CLI config" button.

**Tech Stack:** Go 1.22, `net/http`, `encoding/base64`, `os/exec`, `node --test` (zero deps), existing `llm.Claude` runner pattern.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `mcp/config/config.go` | Create | Read/write `config.json`; `UserConfigDir` resolution |
| `mcp/config/config_test.go` | Create | Round-trip, missing file, path construction |
| `mcp/github/client.go` | Create | `GetFile`, `CommitFile`, `PutContent` via REST |
| `mcp/github/client_test.go` | Create | Mock HTTP, 404→exists:false, 409 retry, success |
| `mcp/hook/render.go` | Create | Go port of `renderEntry` markdown block builder |
| `mcp/hook/handler.go` | Create | `RunSessionEnd` / `RunPreCompact` orchestration |
| `mcp/hook/handler_test.go` | Create | Fake runner + fake GitHub, transcript parsing |
| `mcp/prompts/session.txt` | Create | System prompt for session summarization |
| `mcp/prompts/embed.go` | Modify | Add `//go:embed session.txt` |
| `mcp/server/handlers.go` | Modify | Add `POST /v1/export-config` handler |
| `mcp/main.go` | Modify | `--once` flag detection and dispatch |
| `frontend/src/pages/setup.js` | Modify | check_first toggle + Export to CLI config button |
| `Makefile` | Create | `install` target for binary |
| `README.md` | Modify | Hook wiring instructions |

---

## Task 1: Config Package

**Files:**
- Create: `mcp/config/config.go`
- Create: `mcp/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// mcp/config/config_test.go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/config"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Token:      "ghp_test",
		Owner:      "alice",
		Repo:       "kb",
		CheckFirst: true,
	}
	path := filepath.Join(dir, "config.json")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := config.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != cfg {
		t.Fatalf("got %+v, want %+v", got, cfg)
	}
}

func TestReadMissing(t *testing.T) {
	_, err := config.Read("/nonexistent/path/config.json")
	if !config.IsMissing(err) {
		t.Fatalf("expected missing error, got %v", err)
	}
}

func TestWriteCreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.json")
	if err := config.Write(path, config.Config{Token: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestConfigPath(t *testing.T) {
	p := config.DefaultPath()
	if p == "" {
		t.Fatal("empty path")
	}
	// must end in team-memory/config.json
	if filepath.Base(p) != "config.json" {
		t.Fatalf("unexpected filename: %s", p)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd mcp && go test ./config/...
```
Expected: compile error (package does not exist).

- [ ] **Step 3: Implement config.go**

```go
// mcp/config/config.go
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Config struct {
	Token      string `json:"token"`
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	CheckFirst bool   `json:"check_first"`
}

type missingError struct{ path string }

func (e *missingError) Error() string { return "team-memory: no config found at " + e.path }

func IsMissing(err error) bool {
	var m *missingError
	return errors.As(err, &m)
}

func DefaultPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "team-memory", "config.json")
}

func Read(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, &missingError{path: path}
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	return cfg, json.Unmarshal(data, &cfg)
}

func Write(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
```

- [ ] **Step 4: Run tests — expect pass**

```
cd mcp && go test ./config/...
```

- [ ] **Step 5: Commit**

```bash
git add mcp/config/
git commit -m "feat(config): read/write config.json with UserConfigDir resolution"
```

---

## Task 2: GitHub Client

**Files:**
- Create: `mcp/github/client.go`
- Create: `mcp/github/client_test.go`

- [ ] **Step 1: Write failing tests**

```go
// mcp/github/client_test.go
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

func newCfg(url string) config.Config {
	return config.Config{Token: "tok", Owner: "o", Repo: "r"}
}

func TestGetFile404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	c := gh.NewClient(newCfg(srv.URL), srv.URL)
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

	c := gh.NewClient(newCfg(srv.URL), srv.URL)
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

	c := gh.NewClient(newCfg(srv.URL), srv.URL)
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

	c := gh.NewClient(newCfg(srv.URL), srv.URL)
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

	c := gh.NewClient(newCfg(srv.URL), srv.URL)
	err := c.CommitFile(context.Background(), "f.md", "data", "msg")
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict error, got %v", err)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd mcp && go test ./github/...
```

- [ ] **Step 3: Implement client.go**

```go
// mcp/github/client.go
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/AndrewSkea/team-memory/mcp/config"
)

type FileInfo struct {
	SHA     string
	Content string
	Exists  bool
}

type Client struct {
	cfg     config.Config
	baseURL string
	http    *http.Client
}

func NewClient(cfg config.Config, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return &Client{cfg: cfg, baseURL: baseURL, http: &http.Client{}}
}

func (c *Client) repoURL(path string) string {
	return fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.baseURL, c.cfg.Owner, c.cfg.Repo, path)
}

func (c *Client) do(ctx context.Context, method, url string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

func (c *Client) GetFile(ctx context.Context, path string) (FileInfo, error) {
	resp, err := c.do(ctx, http.MethodGet, c.repoURL(path), nil)
	if err != nil {
		return FileInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return FileInfo{Exists: false}, nil
	}
	if resp.StatusCode >= 400 {
		return FileInfo{}, fmt.Errorf("github GET %s: status %d", path, resp.StatusCode)
	}
	var obj struct {
		SHA     string `json:"sha"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		return FileInfo{}, err
	}
	// GitHub wraps base64 in newlines
	raw := strings.ReplaceAll(obj.Content, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return FileInfo{}, fmt.Errorf("base64 decode: %w", err)
	}
	return FileInfo{SHA: obj.SHA, Content: string(decoded), Exists: true}, nil
}

func (c *Client) CommitFile(ctx context.Context, path, appendContent, message string) error {
	for attempt := range 3 {
		f, err := c.GetFile(ctx, path)
		if err != nil {
			return err
		}
		content := f.Content + appendContent
		body := map[string]any{
			"message": message,
			"content": base64.StdEncoding.EncodeToString([]byte(content)),
		}
		if f.Exists {
			body["sha"] = f.SHA
		}
		resp, err := c.do(ctx, http.MethodPut, c.repoURL(path), body)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode == 409 {
			if attempt == 2 {
				return fmt.Errorf("team-memory: conflict writing %s after 3 retries", path)
			}
			continue
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("github PUT %s: status %d", path, resp.StatusCode)
		}
		return nil
	}
	return fmt.Errorf("team-memory: conflict writing %s after 3 retries", path)
}

func (c *Client) PutContent(ctx context.Context, path, content, message string) error {
	f, err := c.GetFile(ctx, path)
	if err != nil {
		return err
	}
	body := map[string]any{
		"message": message,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
	}
	if f.Exists {
		body["sha"] = f.SHA
	}
	resp, err := c.do(ctx, http.MethodPut, c.repoURL(path), body)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("github PUT %s: status %d", path, resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run tests — expect pass**

```
cd mcp && go test ./github/...
```

- [ ] **Step 5: Commit**

```bash
git add mcp/github/
git commit -m "feat(github): REST client with GetFile, CommitFile (SHA-retry), PutContent"
```

---

## Task 3: Entry Renderer (Go port of renderEntry)

**Files:**
- Create: `mcp/hook/render.go`
- Create: `mcp/hook/render_test.go`

- [ ] **Step 1: Write failing tests**

```go
// mcp/hook/render_test.go
package hook_test

import (
	"strings"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/hook"
)

func TestRenderEntry(t *testing.T) {
	e := hook.Entry{
		Timestamp:  "2026-05-05T12:00:00Z",
		ShortTitle: "test session",
		Scope:      "Team",
		Type:       "General",
		Tags:       "go;testing",
		Source:     "Stop",
		Summary:    "Implemented something",
		Bullets:    []string{"did A", "did B"},
		Full:       "Implemented something",
	}
	got := hook.RenderEntry(e)
	want := []string{
		"### Entry: 2026-05-05T12:00:00Z — test session",
		"**Scope:** Team",
		"**Type:** General",
		"**Tags:** go;testing",
		"**Source:** Stop",
		"**Summary:** Implemented something",
		"**Bullets:** - did A ; - did B",
		"**Full:**",
		"Implemented something",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func TestRenderEntryNoBullets(t *testing.T) {
	e := hook.Entry{
		Timestamp:  "2026-05-05T12:00:00Z",
		ShortTitle: "empty",
		Source:     "PreCompact",
	}
	got := hook.RenderEntry(e)
	if !strings.Contains(got, "**Bullets:**") {
		t.Error("missing Bullets line")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd mcp && go test ./hook/...
```

- [ ] **Step 3: Implement render.go**

```go
// mcp/hook/render.go
package hook

import (
	"fmt"
	"strings"
)

type Entry struct {
	Timestamp  string
	ShortTitle string
	Scope      string
	Type       string
	Tags       string
	Source     string
	Summary    string
	Bullets    []string
	Full       string
}

func RenderEntry(e Entry) string {
	bullets := make([]string, len(e.Bullets))
	for i, b := range e.Bullets {
		bullets[i] = "- " + b
	}
	bulletsStr := strings.Join(bullets, " ; ")
	lines := []string{
		fmt.Sprintf("### Entry: %s — %s", e.Timestamp, e.ShortTitle),
		fmt.Sprintf("**Scope:** %s", e.Scope),
		fmt.Sprintf("**Type:** %s", e.Type),
		fmt.Sprintf("**Tags:** %s", e.Tags),
		fmt.Sprintf("**Source:** %s", e.Source),
		fmt.Sprintf("**Summary:** %s", e.Summary),
		fmt.Sprintf("**Bullets:** %s", bulletsStr),
		"**Full:**",
		e.Full,
		"",
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 4: Run tests — expect pass**

```
cd mcp && go test ./hook/...
```

- [ ] **Step 5: Commit**

```bash
git add mcp/hook/render.go mcp/hook/render_test.go
git commit -m "feat(hook): Go port of renderEntry markdown block builder"
```

---

## Task 4: Session Prompt

**Files:**
- Create: `mcp/prompts/session.txt`
- Modify: `mcp/prompts/embed.go`

- [ ] **Step 1: Write session.txt**

```
mcp/prompts/session.txt
```

Content:
```
You are a team memory assistant. You will be given a Claude Code session transcript (a list of role/content messages) followed by the current INDEX.md content.

Your task:
1. Read the transcript and identify: the primary topic, key decisions made, things learned or built.
2. Pick a target_file from INDEX.md that best fits the content. Follow existing naming conventions. If no good match, use "GENERAL.md".
3. Set unsure: true if the session was trivial (e.g., just reading files, no decisions), ambiguous, or too short to summarize meaningfully.
4. Output ONLY valid JSON matching this schema exactly:

{
  "target_file": "path/in/repo.md",
  "short_title": "3-8 word title",
  "one_sentence_summary": "One sentence.",
  "bullets": ["key point 1", "key point 2", "key point 3"],
  "tags": "tag1;tag2;tag3",
  "unsure": false
}

Rules:
- Output ONLY the JSON object. No markdown fences, no explanation.
- short_title: 3-8 words, sentence case, no trailing period.
- one_sentence_summary: one sentence, ends with period.
- bullets: 2-5 items, each a short phrase.
- tags: semicolon-separated lowercase keywords.
- unsure: true only for trivial or ambiguous sessions.
```

- [ ] **Step 2: Update embed.go**

```go
// mcp/prompts/embed.go
package prompts

import _ "embed"

//go:embed categorize.txt
var Categorize string

//go:embed summarize.txt
var Summarize string

//go:embed session.txt
var Session string
```

- [ ] **Step 3: Run existing embed tests — expect pass**

```
cd mcp && go test ./prompts/...
```

- [ ] **Step 4: Commit**

```bash
git add mcp/prompts/session.txt mcp/prompts/embed.go
git commit -m "feat(prompts): session.txt system prompt for session summarization"
```

---

## Task 5: Hook Handler

**Files:**
- Create: `mcp/hook/handler.go`
- Create: `mcp/hook/handler_test.go` (extend existing render_test.go file)

- [ ] **Step 1: Write failing tests in handler_test.go**

```go
// mcp/hook/handler_test.go
package hook_test

import (
	"context"
	"strings"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/config"
	"github.com/AndrewSkea/team-memory/mcp/hook"
)

// fakeRunner returns preset JSON output
type fakeRunner struct{ out string }

func (f *fakeRunner) Run(_ context.Context, _ string) (string, error) { return f.out, nil }

// fakeGH records calls and returns preset values
type fakeGH struct {
	indexContent string
	committed    []string
	putPaths     []string
}

func (g *fakeGH) GetFile(_ context.Context, path string) (hook.FileInfo, error) {
	if path == "INDEX.md" {
		return hook.FileInfo{Content: g.indexContent, Exists: true, SHA: "abc"}, nil
	}
	return hook.FileInfo{Exists: false}, nil
}

func (g *fakeGH) CommitFile(_ context.Context, path, append, msg string) error {
	g.committed = append(g.committed, path)
	return nil
}

func (g *fakeGH) PutContent(_ context.Context, path, content, msg string) error {
	g.putPaths = append(g.putPaths, path)
	return nil
}

func TestRunSessionEndHappyPath(t *testing.T) {
	runner := &fakeRunner{out: `{
		"target_file": "GENERAL.md",
		"short_title": "tested the thing",
		"one_sentence_summary": "We tested stuff.",
		"bullets": ["wrote tests", "fixed bug"],
		"tags": "testing;go",
		"unsure": false
	}`}
	gh := &fakeGH{indexContent: "GENERAL.md | shared | general\n"}
	transcript := []hook.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	err := hook.RunSessionEnd(context.Background(), config.Config{}, transcript, runner, gh)
	if err != nil {
		t.Fatal(err)
	}
	if len(gh.committed) != 1 || gh.committed[0] != "GENERAL.md" {
		t.Fatalf("expected commit to GENERAL.md, got %v", gh.committed)
	}
}

func TestRunSessionEndEmptyTranscript(t *testing.T) {
	runner := &fakeRunner{}
	gh := &fakeGH{}
	err := hook.RunSessionEnd(context.Background(), config.Config{}, nil, runner, gh)
	if err != nil {
		t.Fatal(err)
	}
	if len(gh.committed) != 0 {
		t.Fatal("should not commit for empty transcript")
	}
}

func TestRunSessionEndNewFileUpdatesIndex(t *testing.T) {
	runner := &fakeRunner{out: `{
		"target_file": "new-topic.md",
		"short_title": "new topic",
		"one_sentence_summary": "Created new topic.",
		"bullets": ["a"],
		"tags": "new",
		"unsure": false
	}`}
	// INDEX does not contain new-topic.md
	gh := &fakeGH{indexContent: "GENERAL.md | shared | general\n"}
	transcript := []hook.Message{{Role: "user", Content: "content"}}
	err := hook.RunSessionEnd(context.Background(), config.Config{}, transcript, runner, gh)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(gh.putPaths, ","), "INDEX.md") {
		t.Fatal("expected INDEX.md update")
	}
}

func TestTruncateTranscript(t *testing.T) {
	msgs := make([]hook.Message, 100)
	for i := range msgs {
		msgs[i] = hook.Message{Role: "user", Content: "msg"}
	}
	got := hook.TruncateTranscript(msgs, 80)
	if len(got) != 80 {
		t.Fatalf("expected 80, got %d", len(got))
	}
	// should be last 80
	if got[0] != msgs[20] {
		t.Fatal("expected last 80 messages")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd mcp && go test ./hook/...
```

- [ ] **Step 3: Implement handler.go**

```go
// mcp/hook/handler.go
package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/config"
	"github.com/AndrewSkea/team-memory/mcp/prompts"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type FileInfo struct {
	SHA     string
	Content string
	Exists  bool
}

type Runner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

type GitHubClient interface {
	GetFile(ctx context.Context, path string) (FileInfo, error)
	CommitFile(ctx context.Context, path, appendContent, message string) error
	PutContent(ctx context.Context, path, content, message string) error
}

type categorizeResult struct {
	TargetFile        string   `json:"target_file"`
	ShortTitle        string   `json:"short_title"`
	OneSentenceSummary string  `json:"one_sentence_summary"`
	Bullets           []string `json:"bullets"`
	Tags              string   `json:"tags"`
	Unsure            bool     `json:"unsure"`
}

func TruncateTranscript(msgs []Message, max int) []Message {
	if len(msgs) <= max {
		return msgs
	}
	return msgs[len(msgs)-max:]
}

func transcriptText(msgs []Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

func runOnce(ctx context.Context, cfg config.Config, transcript []Message, source string, runner Runner, gh GitHubClient) error {
	if len(transcript) == 0 {
		return nil
	}
	transcript = TruncateTranscript(transcript, 80)

	idxFile, err := gh.GetFile(ctx, "INDEX.md")
	if err != nil {
		return fmt.Errorf("fetch INDEX.md: %w", err)
	}
	indexContent := idxFile.Content

	prompt := prompts.Session +
		"\n\nTRANSCRIPT:\n" + transcriptText(transcript) +
		"\n\nINDEX:\n" + indexContent

	out, err := runner.Run(ctx, prompt)
	if err != nil {
		return err
	}
	out = strings.TrimSpace(out)
	if strings.HasPrefix(out, "```") {
		if i := strings.Index(out, "\n"); i >= 0 {
			out = out[i+1:]
		}
		if j := strings.LastIndex(out, "```"); j >= 0 {
			out = out[:j]
		}
		out = strings.TrimSpace(out)
	}

	var result categorizeResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return fmt.Errorf("parse claude output: %w (raw: %.300s)", err, out)
	}

	target := result.TargetFile
	if result.Unsure || target == "" {
		target = "UNSURE.md"
	}

	entry := Entry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		ShortTitle: result.ShortTitle,
		Scope:      "Team",
		Type:       "General",
		Tags:       result.Tags,
		Source:     source,
		Summary:    result.OneSentenceSummary,
		Bullets:    result.Bullets,
		Full:       result.OneSentenceSummary,
	}
	block := "\n" + RenderEntry(entry)

	if err := gh.CommitFile(ctx, target, block, "team-memory: add entry to "+target); err != nil {
		return err
	}

	// Update INDEX if target not listed
	if !strings.Contains(indexContent, target) {
		newIndex := strings.TrimRight(indexContent, "\n") + "\n" + target + " | shared | general\n"
		if err := gh.PutContent(ctx, "INDEX.md", newIndex, "team-memory: update INDEX.md"); err != nil {
			return err
		}
	}
	return nil
}

func RunSessionEnd(ctx context.Context, cfg config.Config, transcript []Message, runner Runner, gh GitHubClient) error {
	return runOnce(ctx, cfg, transcript, "Stop", runner, gh)
}

func RunPreCompact(ctx context.Context, cfg config.Config, transcript []Message, runner Runner, gh GitHubClient) error {
	return runOnce(ctx, cfg, transcript, "PreCompact", runner, gh)
}
```

- [ ] **Step 4: Run tests — expect pass**

```
cd mcp && go test ./hook/...
```

- [ ] **Step 5: Commit**

```bash
git add mcp/hook/handler.go mcp/hook/handler_test.go
git commit -m "feat(hook): RunSessionEnd/RunPreCompact one-shot session summarizer"
```

---

## Task 6: /v1/export-config Endpoint

**Files:**
- Modify: `mcp/server/handlers.go`
- Modify: `mcp/server/handlers_test.go`

- [ ] **Step 1: Write failing test**

Add to `mcp/server/handlers_test.go`:

```go
func TestExportConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	srv := New(Config{Runner: &fakeRunner{}, ConfigPath: cfgPath})
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
```

Also add `"os"`, `"path/filepath"`, `"strings"` imports as needed to the test file.

- [ ] **Step 2: Add ConfigPath to server.Config**

In `mcp/server/server.go`, add `ConfigPath string` to `Config` struct.

```go
type Config struct {
	Runner     llm.Runner
	ConfigPath string // empty = use config.DefaultPath()
}
```

- [ ] **Step 3: Add route and handler in handlers.go**

In `routes()`:
```go
s.mux.HandleFunc("/v1/export-config", s.handleExportConfig)
```

Add handler:
```go
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
```

Add import: `"github.com/AndrewSkea/team-memory/mcp/config"`

- [ ] **Step 4: Run all server tests — expect pass**

```
cd mcp && go test ./server/...
```

- [ ] **Step 5: Commit**

```bash
git add mcp/server/handlers.go mcp/server/handlers_test.go mcp/server/server.go
git commit -m "feat(server): POST /v1/export-config writes CLI config file"
```

---

## Task 7: --once Flag in main.go

**Files:**
- Modify: `mcp/main.go`

- [ ] **Step 1: Read current main.go for context**

Current content:
```go
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/AndrewSkea/team-memory/mcp/llm"
	"github.com/AndrewSkea/team-memory/mcp/server"
)

func main() {
	port := flag.String("port", "7438", "loopback port")
	flag.Parse()
	runner := llm.NewClaude("")
	srv := server.New(server.Config{Runner: runner})
	addr := "127.0.0.1:" + *port
	log.Printf("team-memory-mcp listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, server.WithCORS(srv.Handler())))
}
```

- [ ] **Step 2: Write the updated main.go**

```go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/AndrewSkea/team-memory/mcp/config"
	gh "github.com/AndrewSkea/team-memory/mcp/github"
	"github.com/AndrewSkea/team-memory/mcp/hook"
	"github.com/AndrewSkea/team-memory/mcp/llm"
	"github.com/AndrewSkea/team-memory/mcp/server"
	"context"
)

func main() {
	once := flag.String("once", "", "run one-shot command (session-end|precompact) and exit")
	port := flag.String("port", "7438", "loopback port")
	flag.Parse()

	if *once != "" {
		if err := runOnce(*once); err != nil {
			fmt.Fprintf(os.Stderr, "team-memory: %v\n", err)
			os.Exit(1)
		}
		return
	}

	runner := llm.NewClaude("")
	srv := server.New(server.Config{Runner: runner})
	addr := "127.0.0.1:" + *port
	log.Printf("team-memory-mcp listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, server.WithCORS(srv.Handler())))
}

type hookEvent struct {
	Transcript []hook.Message `json:"transcript"`
}

func runOnce(cmd string) error {
	if cmd != "session-end" && cmd != "precompact" {
		return fmt.Errorf("unknown --once command %q (want session-end|precompact)", cmd)
	}

	var event hookEvent
	if err := json.NewDecoder(os.Stdin).Decode(&event); err != nil {
		// If stdin is empty or not JSON, treat as empty transcript
		event.Transcript = nil
	}
	if len(event.Transcript) == 0 {
		return nil
	}

	cfg, err := config.Read(config.DefaultPath())
	if err != nil {
		if config.IsMissing(err) {
			return fmt.Errorf("no config found — open the web UI, complete Setup, click 'Export to CLI config'")
		}
		return err
	}

	client := gh.NewClient(cfg, "")
	runner := llm.NewClaude("")
	ctx := context.Background()

	switch cmd {
	case "session-end":
		return hook.RunSessionEnd(ctx, cfg, event.Transcript, runner, client)
	case "precompact":
		return hook.RunPreCompact(ctx, cfg, event.Transcript, runner, client)
	}
	return nil
}
```

- [ ] **Step 3: Fix github.Client to implement hook.GitHubClient interface**

The `hook.GitHubClient` interface expects `GetFile` returning `hook.FileInfo`. But `github.Client.GetFile` returns `github.FileInfo`. They are different types. We need to make `github.FileInfo` = `hook.FileInfo` or have `github.Client` implement the interface.

**Solution:** Move `FileInfo` to `hook` package and have `github` package use `hook.FileInfo`. OR have `github.Client` return its own `FileInfo` and write an adapter in main.go.

**Simpler:** Define `hook.GitHubClient` interface to use `github.FileInfo` by importing github from hook. But that creates a cycle.

**Best:** Keep `github.FileInfo` in github package. In hook package, define an interface that uses its own `FileInfo` identical struct. In main.go, wrap `*github.Client` with a thin adapter.

Add to `mcp/main.go`:

```go
// githubAdapter adapts *gh.Client to hook.GitHubClient
type githubAdapter struct{ c *gh.Client }

func (a *githubAdapter) GetFile(ctx context.Context, path string) (hook.FileInfo, error) {
	f, err := a.c.GetFile(ctx, path)
	if err != nil {
		return hook.FileInfo{}, err
	}
	return hook.FileInfo{SHA: f.SHA, Content: f.Content, Exists: f.Exists}, nil
}

func (a *githubAdapter) CommitFile(ctx context.Context, path, appendContent, message string) error {
	return a.c.CommitFile(ctx, path, appendContent, message)
}

func (a *githubAdapter) PutContent(ctx context.Context, path, content, message string) error {
	return a.c.PutContent(ctx, path, content, message)
}
```

And in `runOnce`, use `&githubAdapter{c: client}` instead of `client` directly.

- [ ] **Step 4: Build to verify compilation**

```
cd mcp && go build .
```
Expected: no errors.

- [ ] **Step 5: Test with empty stdin**

```powershell
echo "" | .\team-memory-mcp.exe --once session-end
```
Expected: exits 0 silently (empty transcript).

- [ ] **Step 6: Commit**

```bash
git add mcp/main.go
git commit -m "feat(main): --once session-end|precompact one-shot hook mode"
```

---

## Task 8: Frontend — Setup Page (check_first + Export button)

**Files:**
- Modify: `frontend/src/pages/setup.js`

- [ ] **Step 1: Read current setup.js**

Current file ends after the `onDone(next)` call. The new fields go below the "Save & verify" button section, shown only after successful verification.

- [ ] **Step 2: Update renderSetup**

Replace the entire `frontend/src/pages/setup.js`:

```js
import { GitHubClient } from "../services/github.js";

const SEED_INDEX = `# INDEX for team-memory
GENERAL.md | shared | general
UNSURE.md | shared | unsure
`;

export function renderSetup(root, { onDone, config }) {
  const checkFirst = config.check_first ?? false;
  root.innerHTML = `
    <div class="card">
      <h2>Setup</h2>
      <label>GitHub Personal Access Token <span class="muted">(fine-grained, contents:write on one repo)</span></label>
      <input id="pat" type="password" value="${config.token ?? ""}" />
      <label>Repository (owner/name)</label>
      <input id="repo" type="text" placeholder="AndrewSkea/my-knowledge" value="${config.repo ?? ""}" />
      <label>Anthropic API key <span class="muted">(optional — uses local MCP if blank)</span></label>
      <input id="anthropic" type="password" value="${config.anthropicKey ?? ""}" />
      <p class="muted">Credentials are stored only in this browser's localStorage. Treat this profile as the trust boundary.</p>
      <button class="primary" id="save">Save & verify</button>
      <div id="status" class="muted"></div>
      <div id="cli-section" style="display:none; margin-top:16px; border-top:1px solid var(--border); padding-top:16px;">
        <label style="display:flex; align-items:center; gap:8px; cursor:pointer;">
          <input type="checkbox" id="check-first" ${checkFirst ? "checked" : ""} />
          Check first before saving <span class="muted">(browser Remember page only)</span>
        </label>
        <button id="export-cfg" style="margin-top:12px;">Export to CLI config</button>
        <div id="export-status" class="muted" style="margin-top:4px;"></div>
        <p class="muted" style="margin-top:12px;">Then add to <code>~/.claude/settings.json</code>:</p>
        <pre style="background:var(--bg);padding:8px;border-radius:4px;font-size:12px;overflow-x:auto;">{
  "hooks": {
    "Stop": [{"matcher":"","hooks":[{"type":"command","command":"team-memory-mcp --once session-end"}]}],
    "PreCompact": [{"matcher":"","hooks":[{"type":"command","command":"team-memory-mcp --once precompact"}]}]
  }
}</pre>
      </div>
    </div>
  `;

  const $ = sel => root.querySelector(sel);

  $("#save").onclick = async () => {
    const status = $("#status");
    const token = $("#pat").value.trim();
    const repoStr = $("#repo").value.trim();
    const anthropicKey = $("#anthropic").value.trim();
    if (!token || !repoStr.includes("/")) {
      status.textContent = "Token and owner/repo are required.";
      return;
    }
    const [owner, repo] = repoStr.split("/");
    const gh = new GitHubClient({ token, owner, repo });
    try {
      status.textContent = "Verifying token…";
      const user = await gh.getUser();
      status.textContent = `Authenticated as ${user.login}. Checking repo…`;
      const idx = await gh.getFile("INDEX.md");
      if (!idx.exists) {
        await gh.putContent({ path: "INDEX.md", content: SEED_INDEX, message: "team-memory: seed INDEX.md" });
        await gh.putContent({ path: "GENERAL.md", content: "# GENERAL\n", message: "team-memory: seed GENERAL.md" });
        await gh.putContent({ path: "UNSURE.md", content: "# UNSURE\n", message: "team-memory: seed UNSURE.md" });
        status.textContent = "Seeded fresh repo.";
      }
      const checkFirst = $("#check-first")?.checked ?? false;
      const next = { token, owner, repo, anthropicKey, username: user.login, check_first: checkFirst };
      onDone(next);
      $("#cli-section").style.display = "";
    } catch (e) {
      status.textContent = "Error: " + e.message;
    }
  };

  // Show CLI section immediately if already configured
  if (config.token && config.owner && config.repo) {
    $("#cli-section").style.display = "";
  }

  $("#check-first").onchange = () => {
    // Persist immediately so it survives page reload
    const stored = JSON.parse(localStorage.getItem("team-memory-config") ?? "{}");
    stored.check_first = $("#check-first").checked;
    localStorage.setItem("team-memory-config", JSON.stringify(stored));
  };

  $("#export-cfg").onclick = async () => {
    const status = $("#export-status");
    const token = $("#pat").value.trim();
    const repoStr = $("#repo").value.trim();
    if (!token || !repoStr.includes("/")) {
      status.textContent = "Save & verify first.";
      return;
    }
    const [owner, repo] = repoStr.split("/");
    const checkFirst = $("#check-first").checked;
    try {
      status.textContent = "Exporting…";
      const res = await fetch("http://127.0.0.1:7438/v1/export-config", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token, owner, repo, check_first: checkFirst }),
      });
      if (!res.ok) {
        const j = await res.json().catch(() => ({}));
        status.textContent = "✗ " + (j.error ?? `HTTP ${res.status}`);
        return;
      }
      const j = await res.json();
      status.textContent = "✓ Config saved to " + j.path;
    } catch {
      status.textContent = "✗ MCP not running — start it first";
    }
  };
}
```

- [ ] **Step 3: Run frontend tests to confirm no regressions**

```
cd frontend && node --test tests/
```
Expected: all 16 tests pass.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/setup.js
git commit -m "feat(setup): check_first toggle and Export to CLI config button"
```

---

## Task 9: Remember Page — check_first Confirmation

**Files:**
- Modify: `frontend/src/pages/remember.js`

- [ ] **Step 1: Add check_first confirmation to save handler**

In `renderRemember`, the `config` object is passed in. Read `config.check_first`. When true and no `preset` (auto-categorize not run), show a confirmation card before saving.

Locate the `$("#save").onclick` handler. Before the main save logic, add:

```js
// check_first guard: confirm if no preset and check_first enabled
const preset = $("#save").dataset.preset ? JSON.parse($("#save").dataset.preset) : null;
if (config.check_first && !preset) {
  const existing = root.querySelector("#check-first-confirm");
  if (!existing) {
    const confirm = document.createElement("div");
    confirm.id = "check-first-confirm";
    confirm.className = "result";
    confirm.innerHTML = `Save this text to GENERAL.md as an unstructured entry? <button id="cfyes" class="primary" style="margin-left:8px;">Save</button> <button id="cfno" style="margin-left:4px;">Cancel</button>`;
    root.querySelector(".card").appendChild(confirm);
    root.querySelector("#cfno").onclick = () => confirm.remove();
    root.querySelector("#cfyes").onclick = () => { confirm.remove(); doSave(); };
  }
  saveBtn.textContent = "Save";
  saveBtn.disabled = false;
  autoBtn.disabled = false;
  return;
}
doSave();
```

Extract the current save body into a `doSave()` function inside the onclick closure.

Full updated `$("#save").onclick`:

```js
  $("#save").onclick = async () => {
    const saveBtn = $("#save");
    const autoBtn = $("#auto");
    saveBtn.textContent = "Saving…";
    saveBtn.disabled = true;
    autoBtn.disabled = true;

    const preset = $("#save").dataset.preset ? JSON.parse($("#save").dataset.preset) : null;

    async function doSave() {
      try {
        const text = $("#text").value.trim();
        if (!text) { toast("Empty memory.", true); return; }
        const type = $("#type").value;
        const scopeSel = $("#scope").value;
        const scope = scopeSel === "Team" ? "Team" : `Personal:${config.username}`;
        const target = $("#target").value.trim() || (type === "Unsure" ? "UNSURE.md" : "GENERAL.md");
        const timestamp = new Date().toISOString();
        const entry = {
          timestamp,
          shortTitle: preset?.short_title ?? text.split("\n")[0].slice(0, 60),
          scope, type,
          tags: preset?.tags ?? "",
          source: "UI",
          summary: preset?.one_sentence_summary ?? "",
          bullets: preset?.bullets ?? [],
          full: text,
        };
        const block = "\n" + renderEntry(entry);
        const res = await gh.commitFile({ path: target, append: block, message: `team-memory: add entry to ${target}` });
        if (!res.ok) {
          if (res.kind === "conflict") {
            toast("Could not save — file changed in GitHub. Open it there and try again.", true);
          } else if (res.kind === "auth") {
            toast("Auth failed — check your PAT.", true);
          } else {
            toast(`Save failed (${res.kind}): ${res.message ?? ""}`, true);
          }
          return;
        }
        const idxFile = await getIndex(gh, cache, true);
        const idx = parseIndex(idxFile.content);
        if (!idx.entries.find(e => e.path === target)) {
          const scopeStr = target.startsWith("users/") ? `personal:${config.username}` : "shared";
          const topics = preset?.tags ?? type.toLowerCase();
          upsertEntry(idx, { path: target, scope: scopeStr, topics });
          await gh.putContent({ path: "INDEX.md", content: serializeIndex(idx), message: "team-memory: update INDEX.md" });
        }
        await cache.delete("file:" + target);
        await cache.delete("file:INDEX.md");
        $("#text").value = "";
        $("#preview").innerHTML = "";
        $("#save").dataset.preset = "";
        toast(`Saved to ${target}.`);
      } catch (e) {
        toast("Save failed: " + e.message, true);
      } finally {
        saveBtn.textContent = "Save";
        saveBtn.disabled = false;
        autoBtn.disabled = false;
      }
    }

    if (config.check_first && !preset) {
      const existing = root.querySelector("#check-first-confirm");
      if (!existing) {
        const card = document.createElement("div");
        card.id = "check-first-confirm";
        card.className = "result";
        card.innerHTML = `Save this text to GENERAL.md as an unstructured entry? <button id="cfyes" class="primary" style="margin-left:8px;">Save</button> <button id="cfno" style="margin-left:4px;">Cancel</button>`;
        $("#preview").replaceWith(card);
        card.querySelector("#cfno").onclick = () => {
          card.replaceWith(Object.assign(document.createElement("div"), { id: "preview" }));
          saveBtn.textContent = "Save";
          saveBtn.disabled = false;
          autoBtn.disabled = false;
        };
        card.querySelector("#cfyes").onclick = () => {
          card.replaceWith(Object.assign(document.createElement("div"), { id: "preview" }));
          doSave();
        };
      } else {
        saveBtn.textContent = "Save";
        saveBtn.disabled = false;
        autoBtn.disabled = false;
      }
      return;
    }

    await doSave();
  };
```

- [ ] **Step 2: Run frontend tests**

```
cd frontend && node --test tests/
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/remember.js
git commit -m "feat(remember): check_first confirmation before unreviewed save"
```

---

## Task 10: Makefile install target + README hook instructions

**Files:**
- Create: `Makefile`
- Modify: `README.md`

- [ ] **Step 1: Create Makefile**

```makefile
# Detect Windows
ifeq ($(OS),Windows_NT)
  EXT := .exe
else
  EXT :=
endif

.PHONY: build install test

build:
	cd mcp && go build -o team-memory-mcp$(EXT) .

install: build
	mkdir -p ~/bin
	cp mcp/team-memory-mcp$(EXT) ~/bin/
	@echo "Installed to ~/bin/team-memory-mcp$(EXT)"
	@echo "Make sure ~/bin is on your PATH"

test:
	cd mcp && go test ./...
	cd frontend && node --test tests/
```

- [ ] **Step 2: Add hook wiring section to README.md**

Open README.md and find the end of the "Quickstart" section. Append a new section:

```markdown
## Claude Code Hook Setup (Slice 2)

After completing Setup in the web UI:

1. **Build and install the binary:**
   ```bash
   make install
   ```
   Ensure `~/bin` is on your `PATH`.

2. **Export CLI config** from the Setup page — click "Export to CLI config" after verifying your token.

3. **Add hooks** to `~/.claude/settings.json`:
   ```json
   {
     "hooks": {
       "Stop": [
         {
           "matcher": "",
           "hooks": [{"type": "command", "command": "team-memory-mcp --once session-end"}]
         }
       ],
       "PreCompact": [
         {
           "matcher": "",
           "hooks": [{"type": "command", "command": "team-memory-mcp --once precompact"}]
         }
       ]
     }
   }
   ```

4. End any Claude Code session — it will auto-save a structured summary to your GitHub repo.
```

- [ ] **Step 3: Run `make build` to verify**

```
make build
```
Expected: `mcp/team-memory-mcp` (or `.exe`) produced with no errors.

- [ ] **Step 4: Commit**

```bash
git add Makefile README.md
git commit -m "feat: Makefile install target and README hook wiring instructions"
```

---

## Task 11: Integration Check + Final Build

**Files:** None (verification only)

- [ ] **Step 1: Run all Go tests**

```
cd mcp && go test ./...
```
Expected: all pass.

- [ ] **Step 2: Run all frontend tests**

```
cd frontend && node --test tests/
```
Expected: 16 tests pass.

- [ ] **Step 3: Build binary**

```
cd mcp && go build .
```
Expected: no errors.

- [ ] **Step 4: Smoke test --once with fake JSON**

```powershell
echo '{"transcript":[]}' | .\mcp\team-memory-mcp.exe --once session-end
```
Expected: exits 0 silently.

```powershell
echo '{}' | .\mcp\team-memory-mcp.exe --once session-end
```
Expected: exits 0 silently (empty transcript).

- [ ] **Step 5: Smoke test config missing error**

```powershell
echo '{"transcript":[{"role":"user","content":"hello"}]}' | .\mcp\team-memory-mcp.exe --once session-end
```
Expected: exits 1 with message about opening web UI (unless config already exists).

- [ ] **Step 6: Final commit if anything changed**

```bash
git status
# commit any remaining changes
```

---

## Self-Review Against Spec

**Spec section → Task coverage:**

| Spec requirement | Task |
|---|---|
| `--once session-end` one-shot command | Task 7 |
| `--once precompact` one-shot command | Task 7 |
| Config file `os.UserConfigDir()/team-memory/config.json` | Task 1 |
| `POST /v1/export-config` endpoint | Task 6 |
| `prompts/session.txt` system prompt | Task 4 |
| Setup page check_first toggle | Task 8 |
| Setup page Export to CLI config button | Task 8 |
| Remember page check_first confirmation | Task 9 |
| Go GitHub client `GetFile/CommitFile/PutContent` | Task 2 |
| SHA-retry 3 attempts on 409 | Task 2 |
| Go port of `renderEntry` | Task 3 |
| `hook/handler.go` RunSessionEnd + RunPreCompact | Task 5 |
| Truncate transcript to last 80 messages | Task 5 |
| Empty transcript → exit 0 silently | Task 5, Task 7 |
| INDEX.md update if target file new | Task 5 |
| Makefile `install` target | Task 10 |
| README hook wiring instructions | Task 10 |
| Error messages per spec error table | Task 7 |
| Config file 0600 permissions | Task 1 |

All spec sections covered.
