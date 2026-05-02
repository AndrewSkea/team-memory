# team-memory Slice 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship slice 1 of team-memory: a static web app + minimal Go MCP that lets a single user save and look up Markdown memories in their GitHub repo, optionally with LLM-driven categorization (browser Anthropic key OR local MCP shelling out to `claude -p`).

**Architecture:** Vanilla-JS static frontend (no framework) + Go MCP binary that wraps `claude -p --output-format stream-json --verbose`. Frontend talks directly to GitHub REST and (optionally) directly to Anthropic; MCP is a loopback-only HTTP service that does nothing but call the LLM. Prompts live once at repo root and are copied into the frontend bundle and embedded into the Go binary.

**Tech Stack:** HTML/CSS/ES modules, `node --test` (zero deps) for frontend tests, `fake-indexeddb` for IndexedDB tests, Go 1.22+, `go test`, `httptest`. No frameworks, no bundlers (slice 1 uses native ES modules served as files).

**Build order rationale:** prompts/bootstrap → MCP (so the LLM path is testable via `curl` before frontend exists) → frontend services bottom-up (TDD-friendly) → frontend pages → app shell → README. This means at every checkpoint there is something runnable and testable.

---

## File map

**Created:**

```
.gitignore
README.md
package.json                                    # npm test + copy-prompts
Makefile                                        # mcp build helper
prompts/categorize.txt
prompts/summarize.txt
mcp/go.mod
mcp/main.go
mcp/server/server.go
mcp/server/handlers.go
mcp/server/handlers_test.go
mcp/llm/claude.go
mcp/llm/claude_test.go
mcp/llm/stream.go
mcp/llm/stream_test.go
mcp/llm/testdata/stream_categorize.jsonl
mcp/llm/testdata/fake_claude.sh
mcp/llm/testdata/fake_claude.cmd
mcp/prompts/embed.go
mcp/prompts/categorize.txt                      # copied from /prompts at build
mcp/prompts/summarize.txt
frontend/index.html
frontend/src/app.js
frontend/src/pages/setup.js
frontend/src/pages/remember.js
frontend/src/pages/lookup.js
frontend/src/services/github.js
frontend/src/services/cache.js
frontend/src/services/indexmd.js
frontend/src/services/entries.js
frontend/src/services/llm/backend.js
frontend/src/services/llm/anthropic.js
frontend/src/services/llm/mcp.js
frontend/src/ui/main.css
frontend/prompts/categorize.txt                 # copied from /prompts at build
frontend/prompts/summarize.txt
tests/frontend/indexmd.test.js
tests/frontend/entries.test.js
tests/frontend/github.test.js
tests/frontend/cache.test.js
scripts/copy-prompts.sh
```

Each file has one responsibility. Frontend services are pure functions where possible (parse, render, classify) and isolate side effects (`fetch`, IndexedDB) into thin wrappers so tests can mock them.

---

## Task 1: Repo bootstrap

**Files:**
- Create: `.gitignore`
- Create: `README.md` (stub)
- Create: `package.json`
- Create: `Makefile`
- Create: `scripts/copy-prompts.sh`

- [ ] **Step 1: Create `.gitignore`**

```
# Node
node_modules/
*.log

# Go
mcp/team-memory-mcp
mcp/team-memory-mcp.exe

# Editor
.vscode/
.idea/
*.swp
.DS_Store
```

- [ ] **Step 2: Create stub `README.md`**

```markdown
# team-memory

Local-first team memory. Slice 1 — see `docs/superpowers/specs/2026-05-02-team-memory-slice-1-design.md` and `ROADMAP.md`.

Quickstart and smoke test live at the bottom of this file once Task 18 is complete.
```

- [ ] **Step 3: Create `package.json`**

```json
{
  "name": "team-memory",
  "private": true,
  "type": "module",
  "scripts": {
    "test": "node --test tests/frontend",
    "copy-prompts": "bash scripts/copy-prompts.sh",
    "serve": "python3 -m http.server 8080 --directory frontend"
  },
  "devDependencies": {
    "fake-indexeddb": "^6.0.0"
  }
}
```

- [ ] **Step 4: Create `scripts/copy-prompts.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail
cp prompts/*.txt frontend/prompts/
cp prompts/*.txt mcp/prompts/
echo "prompts copied to frontend/prompts and mcp/prompts"
```

Then `chmod +x scripts/copy-prompts.sh`.

- [ ] **Step 5: Create `Makefile`**

```makefile
.PHONY: prompts mcp test-mcp test-frontend test

prompts:
	bash scripts/copy-prompts.sh

mcp: prompts
	cd mcp && go build -o team-memory-mcp .

test-mcp:
	cd mcp && go test ./...

test-frontend:
	npm test

test: test-frontend test-mcp
```

- [ ] **Step 6: Commit**

```bash
git add .gitignore README.md package.json Makefile scripts/
git commit -m "chore: repo bootstrap (gitignore, package.json, Makefile, prompt-copy script)"
```

---

## Task 2: Prompt files

**Files:**
- Create: `prompts/categorize.txt`
- Create: `prompts/summarize.txt`
- Create: `frontend/prompts/.gitkeep` (so the dir exists pre-copy)
- Create: `mcp/prompts/.gitkeep`

- [ ] **Step 1: Create `prompts/categorize.txt`** (verbatim from spec)

```
You are an assistant that classifies and summarizes a memory for team-memory. Read INDEX.md (provided) and decide a target file path, short title, tags, a one-sentence summary, and up to 5 bullets. Use topic-specific files; do not create overly broad files. If confidence < 0.7 return unsure:true and target "UNSURE.md". Output JSON only.

INPUT:
INDEX: <INDEX_CONTENT>
PAYLOAD: {"scope":"Team"|"Personal:<name>","type":"General"|"Meeting Notes"|"Unsure"|"Programming"|"Ideas"|"Reminders","text":"<raw text>","source":"UI"|"CLI"|"upload:<filename>","timestamp":"<ISO8601>"}

OUTPUT JSON schema:
{
  "target_file":"path",
  "short_title":"string",
  "one_sentence_summary":"string",
  "bullets":["b1","b2","b3"],
  "tags":"tag1;tag2",
  "entry_header":"Entry: <ISO8601> — <short_title>",
  "unsure":false,
  "rationale":"optional short phrase when unsure"
}
```

- [ ] **Step 2: Create `prompts/summarize.txt`**

```
Summarize the uploaded document. Output JSON only.

INPUT: {"filename":"<name>","text":"<extracted text>"}

OUTPUT:
{"title":"...","one_sentence_summary":"...","bullets":["..."],"target_file":"shared/<topic>.md or users/<name>/<topic>.md","tags":"..."}
```

- [ ] **Step 3: Create dir placeholders**

```bash
mkdir -p frontend/prompts mcp/prompts
touch frontend/prompts/.gitkeep mcp/prompts/.gitkeep
```

- [ ] **Step 4: Run `make prompts` and verify**

```bash
make prompts
ls frontend/prompts mcp/prompts
```

Expected: each dir contains `categorize.txt` and `summarize.txt`.

- [ ] **Step 5: Add copied prompts to gitignore**

Edit `.gitignore` to append:

```
frontend/prompts/*.txt
mcp/prompts/*.txt
```

(So generated copies don't get committed; the source of truth is `prompts/`.)

- [ ] **Step 6: Commit**

```bash
git add prompts/ frontend/prompts/.gitkeep mcp/prompts/.gitkeep .gitignore
git commit -m "feat: add categorize and summarize prompt source-of-truth files"
```

---

## Task 3: MCP scaffold + /health

**Files:**
- Create: `mcp/go.mod`
- Create: `mcp/main.go`
- Create: `mcp/server/server.go`
- Create: `mcp/server/handlers.go`
- Create: `mcp/server/handlers_test.go`

- [ ] **Step 1: Init Go module**

```bash
cd mcp && go mod init github.com/AndrewSkea/team-memory/mcp
```

- [ ] **Step 2: Write the failing test for `/health`**

Create `mcp/server/handlers_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd mcp && go test ./server/...
```

Expected: compile error — `server.New` and `Config` undefined.

- [ ] **Step 4: Implement minimal `server.go` and `handlers.go`**

`mcp/server/server.go`:

```go
package server

import (
	"net/http"
	"os/exec"
)

type Config struct {
	ClaudePath string // override for tests; defaults to "claude" on PATH
}

type Server struct {
	cfg Config
	mux *http.ServeMux
}

func New(cfg Config) *Server {
	if cfg.ClaudePath == "" {
		cfg.ClaudePath = "claude"
	}
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) claudeAvailable() bool {
	_, err := exec.LookPath(s.cfg.ClaudePath)
	return err == nil
}
```

`mcp/server/handlers.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
)

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	claude := "missing"
	if s.claudeAvailable() {
		claude = "available"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"claude": claude,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd mcp && go test ./server/...
```

Expected: PASS.

- [ ] **Step 6: Implement `main.go` (loopback bind, port flag)**

```go
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/AndrewSkea/team-memory/mcp/server"
)

func main() {
	port := flag.String("port", "7438", "loopback port")
	flag.Parse()

	srv := server.New(server.Config{})
	addr := "127.0.0.1:" + *port
	log.Printf("team-memory-mcp listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, withCORS(srv.Handler())))
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func originAllowed(o string) bool {
	if o == "" || o == "null" {
		return true // file://
	}
	// localhost on any port
	return len(o) >= 16 && o[:16] == "http://localhost" ||
		len(o) >= 16 && o[:16] == "http://127.0.0.1"
}
```

- [ ] **Step 7: Build and smoke test**

```bash
cd mcp && go build -o team-memory-mcp .
./team-memory-mcp &
curl -s http://127.0.0.1:7438/health
kill %1
```

Expected: `{"status":"ok","claude":"missing"}` (or `"available"` if `claude` is on PATH).

- [ ] **Step 8: Commit**

```bash
git add mcp/
git commit -m "feat(mcp): scaffold Go server with /health endpoint and loopback CORS"
```

---

## Task 4: MCP — embed prompts

**Files:**
- Create: `mcp/prompts/embed.go`

- [ ] **Step 1: Create `mcp/prompts/embed.go`**

```go
package prompts

import _ "embed"

//go:embed categorize.txt
var Categorize string

//go:embed summarize.txt
var Summarize string
```

- [ ] **Step 2: Verify it compiles after `make prompts`**

```bash
make prompts
cd mcp && go build ./prompts/...
```

Expected: no error.

- [ ] **Step 3: Add a one-line sanity test**

Append to `mcp/prompts/embed.go` a tiny test in `mcp/prompts/embed_test.go`:

```go
package prompts

import "testing"

func TestPromptsEmbedded(t *testing.T) {
	if len(Categorize) < 100 {
		t.Errorf("Categorize prompt too short: %d bytes", len(Categorize))
	}
	if len(Summarize) < 50 {
		t.Errorf("Summarize prompt too short: %d bytes", len(Summarize))
	}
}
```

- [ ] **Step 4: Run test**

```bash
cd mcp && go test ./prompts/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add mcp/prompts/
git commit -m "feat(mcp): embed prompt templates via //go:embed"
```

---

## Task 5: MCP — stream-json reassembly

**Files:**
- Create: `mcp/llm/stream.go`
- Create: `mcp/llm/stream_test.go`
- Create: `mcp/llm/testdata/stream_categorize.jsonl`

The Claude Code CLI emits one JSON object per line when called with `--output-format stream-json --verbose`. Each line is either a `system`, `assistant`, `user`, or `result` event. The final assistant text is what we need.

- [ ] **Step 1: Create the test fixture**

`mcp/llm/testdata/stream_categorize.jsonl`:

```
{"type":"system","subtype":"init","session_id":"abc"}
{"type":"assistant","message":{"content":[{"type":"text","text":"{\"target_file\":\"shared/programming-practices.md\",\"short_title\":\"Enforce semicolon linting\",\"one_sentence_summary\":\"Adopt semi rule.\",\"bullets\":[\"Add ESLint rule\"],\"tags\":\"eslint;style\",\"entry_header\":\"Entry: 2026-05-02T09:48:00Z — Enforce semicolon linting\",\"unsure\":false}"}]}}
{"type":"result","subtype":"success","is_error":false}
```

- [ ] **Step 2: Write the failing test**

`mcp/llm/stream_test.go`:

```go
package llm

import (
	"os"
	"strings"
	"testing"
)

func TestExtractAssistantText(t *testing.T) {
	data, err := os.ReadFile("testdata/stream_categorize.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExtractAssistantText(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"target_file":"shared/programming-practices.md"`) {
		t.Errorf("missing target_file in extracted text:\n%s", got)
	}
	if !strings.Contains(got, `"unsure":false`) {
		t.Errorf("missing unsure field in extracted text:\n%s", got)
	}
}

func TestExtractAssistantTextErrorEvent(t *testing.T) {
	input := `{"type":"system","subtype":"init"}
{"type":"result","subtype":"error","is_error":true,"result":"boom"}
`
	_, err := ExtractAssistantText(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
```

- [ ] **Step 3: Run test — should fail (no impl)**

```bash
cd mcp && go test ./llm/...
```

Expected: compile error.

- [ ] **Step 4: Implement `stream.go`**

```go
package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type streamEvent struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype"`
	IsError bool            `json:"is_error"`
	Result  string          `json:"result"`
	Message json.RawMessage `json:"message"`
}

type assistantMessage struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// ExtractAssistantText reads stream-json (one event per line) and returns the
// concatenated text content of all assistant messages. Returns error if the
// stream contains an error result event.
func ExtractAssistantText(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var b strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev streamEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return "", fmt.Errorf("invalid stream-json line: %w", err)
		}
		switch ev.Type {
		case "assistant":
			var msg assistantMessage
			if err := json.Unmarshal(ev.Message, &msg); err != nil {
				return "", fmt.Errorf("invalid assistant message: %w", err)
			}
			for _, c := range msg.Content {
				if c.Type == "text" {
					b.WriteString(c.Text)
				}
			}
		case "result":
			if ev.IsError {
				return "", fmt.Errorf("claude error: %s", ev.Result)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return b.String(), nil
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd mcp && go test ./llm/...
```

Expected: PASS for both tests.

- [ ] **Step 6: Commit**

```bash
git add mcp/llm/
git commit -m "feat(mcp): parse claude stream-json output into assistant text"
```

---

## Task 6: MCP — `claude` subprocess wrapper

**Files:**
- Create: `mcp/llm/claude.go`
- Create: `mcp/llm/claude_test.go`
- Create: `mcp/llm/testdata/fake_claude.sh`
- Create: `mcp/llm/testdata/fake_claude.cmd`

- [ ] **Step 1: Create the fake `claude` script (POSIX)**

`mcp/llm/testdata/fake_claude.sh`:

```bash
#!/usr/bin/env bash
# Reads the prompt from argv (after `-p`) and prints a fixed stream-json fixture.
# Used to test claude.go without depending on the real claude CLI.
cat "$(dirname "$0")/stream_categorize.jsonl"
```

Then `chmod +x mcp/llm/testdata/fake_claude.sh`.

- [ ] **Step 2: Create the fake `claude.cmd` (Windows)**

`mcp/llm/testdata/fake_claude.cmd`:

```bat
@echo off
type "%~dp0stream_categorize.jsonl"
```

- [ ] **Step 3: Write the failing test**

`mcp/llm/claude_test.go`:

```go
package llm

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestRunClaude_FakeBackend(t *testing.T) {
	script := "testdata/fake_claude.sh"
	if runtime.GOOS == "windows" {
		script = "testdata\\fake_claude.cmd"
	}
	c := NewClaude(script)
	out, err := c.Run(context.Background(), "ignored prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, `"target_file"`) {
		t.Errorf("expected JSON in output, got: %s", out)
	}
}

func TestRunClaude_NotFound(t *testing.T) {
	c := NewClaude("/definitely/not/a/real/binary")
	_, err := c.Run(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}
```

- [ ] **Step 4: Run test — should fail (no impl)**

```bash
cd mcp && go test ./llm/...
```

Expected: compile error on `NewClaude`/`Run`.

- [ ] **Step 5: Implement `claude.go`**

```go
package llm

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Claude struct {
	path string
}

func NewClaude(path string) *Claude {
	if path == "" {
		path = "claude"
	}
	return &Claude{path: path}
}

// Run invokes `claude -p <prompt> --output-format stream-json --verbose` and
// returns the assembled assistant text.
func (c *Claude) Run(ctx context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, c.path,
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(stderr.String())
		if len(tail) > 500 {
			tail = "..." + tail[len(tail)-500:]
		}
		return "", fmt.Errorf("claude failed: %w (stderr: %s)", err, tail)
	}
	text, err := ExtractAssistantText(&stdout)
	if err != nil {
		return "", fmt.Errorf("parse stream-json: %w", err)
	}
	return text, nil
}
```

- [ ] **Step 6: Run test to verify it passes (skip on Windows if no .cmd handler)**

```bash
cd mcp && go test ./llm/...
```

Expected: PASS on POSIX. On Windows, if it fails, mark `TestRunClaude_FakeBackend` with `t.Skip` for `runtime.GOOS == "windows"` only after verifying the issue is environmental.

- [ ] **Step 7: Commit**

```bash
git add mcp/llm/
git commit -m "feat(mcp): claude subprocess wrapper with fake-backend tests"
```

---

## Task 7: MCP — `/v1/categorize` and `/v1/summarize`

**Files:**
- Modify: `mcp/server/server.go` (inject Claude runner)
- Modify: `mcp/server/handlers.go`
- Modify: `mcp/server/handlers_test.go`
- Modify: `mcp/main.go` (wire real Claude)

- [ ] **Step 1: Define the LLM-runner interface in `server.go`**

Edit `mcp/server/server.go` — replace the file with:

```go
package server

import (
	"context"
	"net/http"
	"os/exec"
)

type LLMRunner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

type Config struct {
	ClaudePath string
	Runner     LLMRunner // overrideable for tests
}

type Server struct {
	cfg Config
	mux *http.ServeMux
}

func New(cfg Config) *Server {
	if cfg.ClaudePath == "" {
		cfg.ClaudePath = "claude"
	}
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) claudeAvailable() bool {
	_, err := exec.LookPath(s.cfg.ClaudePath)
	return err == nil
}
```

- [ ] **Step 2: Write the failing test for `/v1/categorize`**

Append to `mcp/server/handlers_test.go`:

```go
import (
	"bytes"
	"context"
	"strings"
)

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
```

- [ ] **Step 3: Run test — should fail**

```bash
cd mcp && go test ./server/...
```

Expected: `/v1/categorize` returns 404.

- [ ] **Step 4: Implement handlers**

Replace `mcp/server/handlers.go`:

```go
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
	// Pass through verbatim — the LLM is contracted to produce JSON.
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
```

- [ ] **Step 5: Wire real Claude in `main.go`**

Update `mcp/main.go` to construct the runner:

Replace the `srv := server.New(server.Config{})` line with:

```go
import "github.com/AndrewSkea/team-memory/mcp/llm"

// ...
runner := llm.NewClaude("")
srv := server.New(server.Config{Runner: runner})
```

(Reorganize imports as needed.)

- [ ] **Step 6: Run all MCP tests**

```bash
cd mcp && go test ./...
```

Expected: all PASS.

- [ ] **Step 7: Smoke-test against real `claude` if available**

```bash
make mcp
./mcp/team-memory-mcp &
sleep 1
curl -s -X POST http://127.0.0.1:7438/v1/categorize \
  -H 'Content-Type: application/json' \
  -d '{"index":"# INDEX\nGENERAL.md | shared | general","payload":{"scope":"Team","type":"General","text":"Test memory about API contracts","source":"UI","timestamp":"2026-05-02T00:00:00Z"},"token_budget":200}'
kill %1
```

Expected: a JSON object matching the categorize schema (or 502 if `claude` is not installed — that's fine, the wiring is verified).

- [ ] **Step 8: Commit**

```bash
git add mcp/
git commit -m "feat(mcp): /v1/categorize and /v1/summarize endpoints with timeout + DI runner"
```

---

## Task 8: Frontend — `indexmd.js` parse/serialize

**Files:**
- Create: `frontend/src/services/indexmd.js`
- Create: `tests/frontend/indexmd.test.js`

`INDEX.md` line format: `path | scope | primary_topics ; secondary_topics ; tags`. The `topics; tags` field after the second `|` is free-form for slice 1; we treat it as one opaque string.

- [ ] **Step 1: Write the failing test**

`tests/frontend/indexmd.test.js`:

```js
import { test } from "node:test";
import assert from "node:assert/strict";
import { parseIndex, serializeIndex, upsertEntry } from "../../frontend/src/services/indexmd.js";

const SAMPLE = `# INDEX for team-memory
shared/programming-practices.md | shared | programming practices; code review; style
GENERAL.md | shared | general
UNSURE.md | shared | unsure
`;

test("parseIndex extracts entries", () => {
  const idx = parseIndex(SAMPLE);
  assert.equal(idx.entries.length, 3);
  assert.deepEqual(idx.entries[0], {
    path: "shared/programming-practices.md",
    scope: "shared",
    topics: "programming practices; code review; style",
  });
});

test("serializeIndex round-trips", () => {
  const idx = parseIndex(SAMPLE);
  const out = serializeIndex(idx);
  assert.equal(parseIndex(out).entries.length, 3);
});

test("upsertEntry adds a new entry", () => {
  const idx = parseIndex(SAMPLE);
  upsertEntry(idx, { path: "shared/new-topic.md", scope: "shared", topics: "new; stuff" });
  assert.equal(idx.entries.length, 4);
  assert.equal(idx.entries[3].path, "shared/new-topic.md");
});

test("upsertEntry replaces existing", () => {
  const idx = parseIndex(SAMPLE);
  upsertEntry(idx, { path: "GENERAL.md", scope: "shared", topics: "general; misc" });
  assert.equal(idx.entries.length, 3);
  assert.equal(idx.entries.find(e => e.path === "GENERAL.md").topics, "general; misc");
});

test("parseIndex tolerates blank lines and missing header", () => {
  const idx = parseIndex("\nGENERAL.md | shared | general\n\n");
  assert.equal(idx.entries.length, 1);
});
```

- [ ] **Step 2: Run — should fail**

```bash
npm test
```

Expected: import error / module not found.

- [ ] **Step 3: Implement `indexmd.js`**

```js
const HEADER = "# INDEX for team-memory";

export function parseIndex(text) {
  const entries = [];
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    const parts = line.split("|").map(p => p.trim());
    if (parts.length < 3) continue;
    entries.push({ path: parts[0], scope: parts[1], topics: parts.slice(2).join(" | ") });
  }
  return { entries };
}

export function serializeIndex(index) {
  const lines = [HEADER];
  for (const e of index.entries) {
    lines.push(`${e.path} | ${e.scope} | ${e.topics}`);
  }
  return lines.join("\n") + "\n";
}

export function upsertEntry(index, entry) {
  const i = index.entries.findIndex(e => e.path === entry.path);
  if (i >= 0) index.entries[i] = entry;
  else index.entries.push(entry);
  return index;
}
```

- [ ] **Step 4: Run tests — should pass**

```bash
npm test
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/services/indexmd.js tests/frontend/indexmd.test.js
git commit -m "feat(frontend): INDEX.md parse/serialize/upsert"
```

---

## Task 9: Frontend — `entries.js` template renderer

**Files:**
- Create: `frontend/src/services/entries.js`
- Create: `tests/frontend/entries.test.js`

- [ ] **Step 1: Write the failing test**

`tests/frontend/entries.test.js`:

```js
import { test } from "node:test";
import assert from "node:assert/strict";
import { renderEntry, appendEntry } from "../../frontend/src/services/entries.js";

const ENTRY = {
  timestamp: "2026-05-02T09:48:00Z",
  shortTitle: "Enforce semicolon linting",
  scope: "Team",
  type: "Programming",
  tags: "eslint;linting;style",
  source: "UI",
  summary: "Proposal to adopt semicolon linting across repos.",
  bullets: ["Add ESLint rule semi:true", "Document in onboarding", "Run autofix in CI"],
  full: "We should adopt a linting rule...",
};

test("renderEntry produces expected markdown shape", () => {
  const md = renderEntry(ENTRY);
  assert.match(md, /^### Entry: 2026-05-02T09:48:00Z — Enforce semicolon linting$/m);
  assert.match(md, /\*\*Scope:\*\* Team/);
  assert.match(md, /\*\*Tags:\*\* eslint;linting;style/);
  assert.match(md, /Add ESLint rule semi:true/);
  assert.match(md, /We should adopt a linting rule/);
});

test("appendEntry preserves existing content with a separator", () => {
  const existing = "# Programming Practices\n\nSome intro.\n";
  const out = appendEntry(existing, ENTRY);
  assert.ok(out.startsWith("# Programming Practices"));
  assert.match(out, /### Entry: 2026-05-02T09:48:00Z/);
});

test("appendEntry handles empty existing content", () => {
  const out = appendEntry("", ENTRY);
  assert.match(out, /### Entry: 2026-05-02T09:48:00Z/);
});
```

- [ ] **Step 2: Run — fail**

```bash
npm test
```

- [ ] **Step 3: Implement `entries.js`**

```js
export function renderEntry(e) {
  const bullets = (e.bullets ?? []).map(b => `- ${b}`).join(" ; ");
  return [
    `### Entry: ${e.timestamp} — ${e.shortTitle}`,
    `**Scope:** ${e.scope}`,
    `**Type:** ${e.type}`,
    `**Tags:** ${e.tags ?? ""}`,
    `**Source:** ${e.source}`,
    `**Summary:** ${e.summary ?? ""}`,
    `**Bullets:** ${bullets}`,
    `**Full:**`,
    e.full ?? "",
    "",
  ].join("\n");
}

export function appendEntry(existing, entry) {
  const block = renderEntry(entry);
  if (!existing || !existing.trim()) return block;
  const sep = existing.endsWith("\n") ? "\n" : "\n\n";
  return existing + sep + block;
}
```

- [ ] **Step 4: Run — pass**

```bash
npm test
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/services/entries.js tests/frontend/entries.test.js
git commit -m "feat(frontend): entry template renderer + append helper"
```

---

## Task 10: Frontend — `github.js` SHA-retry commit

**Files:**
- Create: `frontend/src/services/github.js`
- Create: `tests/frontend/github.test.js`

The GitHub Contents API uses base64 for the `content` field. We `btoa(unescape(encodeURIComponent(text)))` for UTF-8 safety, and `decodeURIComponent(escape(atob(b64)))` for decode.

- [ ] **Step 1: Write the failing test**

`tests/frontend/github.test.js`:

```js
import { test } from "node:test";
import assert from "node:assert/strict";
import { GitHubClient } from "../../frontend/src/services/github.js";

function mockFetch(handlers) {
  const calls = [];
  const fn = async (url, opts = {}) => {
    calls.push({ url, opts });
    const handler = handlers.shift();
    if (!handler) throw new Error("unexpected fetch: " + url);
    return handler({ url, opts });
  };
  fn.calls = calls;
  return fn;
}

function jsonResponse(status, body, headers = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
}

const b64 = s => Buffer.from(s, "utf8").toString("base64");

test("commitFile creates a new file on 404", async () => {
  const fetch = mockFetch([
    () => jsonResponse(404, { message: "Not Found" }),
    () => jsonResponse(201, { content: { sha: "newsha" } }),
  ]);
  const gh = new GitHubClient({ token: "t", owner: "o", repo: "r", fetch });
  const res = await gh.commitFile({ path: "GENERAL.md", append: "hello", message: "msg" });
  assert.equal(res.ok, true);
  assert.equal(fetch.calls.length, 2);
  assert.equal(fetch.calls[1].opts.method, "PUT");
});

test("commitFile retries once on 409 then succeeds", async () => {
  const fetch = mockFetch([
    () => jsonResponse(200, { sha: "s1", content: b64("first\n"), encoding: "base64" }),
    () => jsonResponse(409, { message: "conflict" }),
    () => jsonResponse(200, { sha: "s2", content: b64("first\nsecond\n"), encoding: "base64" }),
    () => jsonResponse(200, { content: { sha: "s3" } }),
  ]);
  const gh = new GitHubClient({ token: "t", owner: "o", repo: "r", fetch });
  const res = await gh.commitFile({ path: "GENERAL.md", append: "third\n", message: "msg" });
  assert.equal(res.ok, true);
  assert.equal(fetch.calls.length, 4);
});

test("commitFile gives up after 3 retries", async () => {
  const fetch = mockFetch([
    () => jsonResponse(200, { sha: "s1", content: b64("a"), encoding: "base64" }),
    () => jsonResponse(409, { message: "c" }),
    () => jsonResponse(200, { sha: "s2", content: b64("a"), encoding: "base64" }),
    () => jsonResponse(409, { message: "c" }),
    () => jsonResponse(200, { sha: "s3", content: b64("a"), encoding: "base64" }),
    () => jsonResponse(409, { message: "c" }),
  ]);
  const gh = new GitHubClient({ token: "t", owner: "o", repo: "r", fetch });
  const res = await gh.commitFile({ path: "GENERAL.md", append: "x", message: "msg" });
  assert.equal(res.ok, false);
  assert.equal(res.kind, "conflict");
});

test("getUser returns login", async () => {
  const fetch = mockFetch([() => jsonResponse(200, { login: "andrew" })]);
  const gh = new GitHubClient({ token: "t", owner: "o", repo: "r", fetch });
  const u = await gh.getUser();
  assert.equal(u.login, "andrew");
});
```

- [ ] **Step 2: Run — fail**

```bash
npm test
```

- [ ] **Step 3: Implement `github.js`**

```js
const API = "https://api.github.com";

function b64encode(str) {
  if (typeof Buffer !== "undefined") return Buffer.from(str, "utf8").toString("base64");
  return btoa(unescape(encodeURIComponent(str)));
}
function b64decode(b64) {
  if (typeof Buffer !== "undefined") return Buffer.from(b64, "base64").toString("utf8");
  return decodeURIComponent(escape(atob(b64)));
}

export class GitHubClient {
  constructor({ token, owner, repo, fetch: f = globalThis.fetch }) {
    this.token = token;
    this.owner = owner;
    this.repo = repo;
    this.fetch = f;
  }

  async _req(path, opts = {}) {
    const res = await this.fetch(`${API}${path}`, {
      ...opts,
      headers: {
        Authorization: `Bearer ${this.token}`,
        Accept: "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
        ...(opts.headers ?? {}),
      },
    });
    return res;
  }

  async getUser() {
    const r = await this._req("/user");
    if (!r.ok) throw new Error(`getUser: ${r.status}`);
    return r.json();
  }

  async getFile(path) {
    const r = await this._req(`/repos/${this.owner}/${this.repo}/contents/${encodeURIComponent(path)}`);
    if (r.status === 404) return { exists: false, sha: null, content: "" };
    if (!r.ok) throw new Error(`getFile ${path}: ${r.status}`);
    const j = await r.json();
    return { exists: true, sha: j.sha, content: b64decode(j.content) };
  }

  async putFile({ path, content, sha, message }) {
    const body = { message, content: b64encode(content) };
    if (sha) body.sha = sha;
    const r = await this._req(`/repos/${this.owner}/${this.repo}/contents/${encodeURIComponent(path)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    return r;
  }

  // commitFile: read-modify-write with SHA-retry up to 3 attempts.
  // `append` is appended to existing content. For full replacement use putFile directly.
  async commitFile({ path, append, message }) {
    for (let attempt = 0; attempt < 3; attempt++) {
      const cur = await this.getFile(path);
      const next = (cur.content ?? "") + append;
      const r = await this.putFile({ path, content: next, sha: cur.sha, message });
      if (r.ok) return { ok: true };
      if (r.status === 409) continue;
      const body = await r.json().catch(() => ({}));
      return { ok: false, kind: classifyStatus(r.status), status: r.status, message: body.message };
    }
    return { ok: false, kind: "conflict", message: "exceeded 3 retries" };
  }

  // putContent: full file replacement (used for INDEX.md updates that aren't pure appends).
  async putContent({ path, content, message }) {
    for (let attempt = 0; attempt < 3; attempt++) {
      const cur = await this.getFile(path);
      const r = await this.putFile({ path, content, sha: cur.sha, message });
      if (r.ok) return { ok: true };
      if (r.status === 409) continue;
      const body = await r.json().catch(() => ({}));
      return { ok: false, kind: classifyStatus(r.status), status: r.status, message: body.message };
    }
    return { ok: false, kind: "conflict", message: "exceeded 3 retries" };
  }
}

function classifyStatus(s) {
  if (s === 401 || s === 403) return "auth";
  if (s === 404) return "not-found";
  if (s === 409) return "conflict";
  if (s === 422) return "invalid";
  return "other";
}
```

- [ ] **Step 4: Run — pass**

```bash
npm test
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/services/github.js tests/frontend/github.test.js
git commit -m "feat(frontend): GitHub REST client with SHA-retry commit"
```

---

## Task 11: Frontend — `cache.js` IndexedDB wrapper

**Files:**
- Create: `frontend/src/services/cache.js`
- Create: `tests/frontend/cache.test.js`

- [ ] **Step 1: Install `fake-indexeddb`**

```bash
npm install
```

(Pulls `fake-indexeddb` declared in Task 1.)

- [ ] **Step 2: Write the failing test**

`tests/frontend/cache.test.js`:

```js
import "fake-indexeddb/auto";
import { test } from "node:test";
import assert from "node:assert/strict";
import { Cache } from "../../frontend/src/services/cache.js";

test("set then get returns the value", async () => {
  const c = new Cache("test1");
  await c.set("k", { hello: "world" });
  const got = await c.get("k");
  assert.deepEqual(got, { hello: "world" });
});

test("get missing key returns undefined", async () => {
  const c = new Cache("test2");
  assert.equal(await c.get("missing"), undefined);
});

test("delete removes the key", async () => {
  const c = new Cache("test3");
  await c.set("k", 1);
  await c.delete("k");
  assert.equal(await c.get("k"), undefined);
});

test("clear empties the store", async () => {
  const c = new Cache("test4");
  await c.set("a", 1);
  await c.set("b", 2);
  await c.clear();
  assert.equal(await c.get("a"), undefined);
  assert.equal(await c.get("b"), undefined);
});
```

- [ ] **Step 3: Run — fail**

```bash
npm test
```

- [ ] **Step 4: Implement `cache.js`**

```js
const STORE = "kv";

function openDB(name) {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(name, 1);
    req.onupgradeneeded = () => req.result.createObjectStore(STORE);
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

function tx(db, mode) {
  return db.transaction(STORE, mode).objectStore(STORE);
}

function promisifyReq(req) {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

export class Cache {
  constructor(dbName = "team-memory") {
    this.dbName = dbName;
    this._db = null;
  }
  async _open() {
    if (!this._db) this._db = await openDB(this.dbName);
    return this._db;
  }
  async get(key) {
    const db = await this._open();
    return promisifyReq(tx(db, "readonly").get(key));
  }
  async set(key, value) {
    const db = await this._open();
    return promisifyReq(tx(db, "readwrite").put(value, key));
  }
  async delete(key) {
    const db = await this._open();
    return promisifyReq(tx(db, "readwrite").delete(key));
  }
  async clear() {
    const db = await this._open();
    return promisifyReq(tx(db, "readwrite").clear());
  }
}
```

- [ ] **Step 5: Run — pass**

```bash
npm test
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/services/cache.js tests/frontend/cache.test.js package-lock.json
git commit -m "feat(frontend): IndexedDB cache wrapper with fake-indexeddb tests"
```

---

## Task 12: Frontend — LLM backends

**Files:**
- Create: `frontend/src/services/llm/backend.js`
- Create: `frontend/src/services/llm/anthropic.js`
- Create: `frontend/src/services/llm/mcp.js`

These are not unit-tested in slice 1 (real network calls; we cover them via the manual smoke check). Code is small and dispatch logic is the only thing worth testing later.

- [ ] **Step 1: Create `backend.js` (interface + factory)**

```js
import { AnthropicDirect } from "./anthropic.js";
import { LocalMCP } from "./mcp.js";

// Loads the categorize prompt (bundled with the static site at /prompts/).
let _promptCache = {};
async function loadPrompt(name) {
  if (_promptCache[name]) return _promptCache[name];
  const r = await fetch(`./prompts/${name}.txt`);
  if (!r.ok) throw new Error(`failed to load prompt ${name}`);
  _promptCache[name] = await r.text();
  return _promptCache[name];
}

// Picks a backend at runtime. Returns null if neither is available.
export async function pickBackend({ anthropicKey, mcpUrl }) {
  if (anthropicKey) return new AnthropicDirect({ apiKey: anthropicKey, loadPrompt });
  const mcp = new LocalMCP({ url: mcpUrl, loadPrompt });
  if (await mcp.healthy()) return mcp;
  return null;
}
```

- [ ] **Step 2: Create `anthropic.js`**

```js
const ANTHROPIC_API = "https://api.anthropic.com/v1/messages";
const MODEL = "claude-sonnet-4-6";

export class AnthropicDirect {
  constructor({ apiKey, loadPrompt, fetch: f = globalThis.fetch }) {
    this.apiKey = apiKey;
    this.loadPrompt = loadPrompt;
    this.fetch = f;
  }

  async _call(systemPrompt, userContent) {
    const res = await this.fetch(ANTHROPIC_API, {
      method: "POST",
      headers: {
        "x-api-key": this.apiKey,
        "anthropic-version": "2023-06-01",
        "anthropic-dangerous-direct-browser-access": "true",
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        model: MODEL,
        max_tokens: 1024,
        system: systemPrompt,
        messages: [{ role: "user", content: userContent }],
      }),
    });
    if (!res.ok) {
      const t = await res.text().catch(() => "");
      throw new Error(`anthropic ${res.status}: ${t.slice(0, 200)}`);
    }
    const j = await res.json();
    const text = (j.content ?? []).filter(c => c.type === "text").map(c => c.text).join("");
    try { return JSON.parse(text); } catch { throw new Error("anthropic returned non-JSON: " + text.slice(0, 200)); }
  }

  async categorize({ index, payload }) {
    const sys = await this.loadPrompt("categorize");
    return this._call(sys, `INDEX:\n${index}\nPAYLOAD:\n${JSON.stringify(payload)}`);
  }

  async summarize({ filename, text }) {
    const sys = await this.loadPrompt("summarize");
    return this._call(sys, JSON.stringify({ filename, text }));
  }

  async healthy() { return true; }
}
```

- [ ] **Step 3: Create `mcp.js`**

```js
export class LocalMCP {
  constructor({ url = "http://127.0.0.1:7438", loadPrompt, fetch: f = globalThis.fetch }) {
    this.url = url;
    this.loadPrompt = loadPrompt;
    this.fetch = f;
  }

  async healthy() {
    try {
      const r = await this.fetch(`${this.url}/health`, { method: "GET" });
      return r.ok;
    } catch {
      return false;
    }
  }

  async categorize({ index, payload }) {
    const r = await this.fetch(`${this.url}/v1/categorize`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ index, payload, token_budget: 200 }),
    });
    if (!r.ok) throw new Error(`mcp ${r.status}: ${await r.text().catch(() => "")}`);
    const text = await r.text();
    try { return JSON.parse(text); } catch { throw new Error("mcp returned non-JSON: " + text.slice(0, 200)); }
  }

  async summarize({ filename, text }) {
    const r = await this.fetch(`${this.url}/v1/summarize`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ filename, text }),
    });
    if (!r.ok) throw new Error(`mcp ${r.status}: ${await r.text().catch(() => "")}`);
    const t = await r.text();
    try { return JSON.parse(t); } catch { throw new Error("mcp returned non-JSON: " + t.slice(0, 200)); }
  }
}
```

- [ ] **Step 4: Verify imports compile**

```bash
node --check frontend/src/services/llm/backend.js
node --check frontend/src/services/llm/anthropic.js
node --check frontend/src/services/llm/mcp.js
```

Expected: no syntax errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/services/llm/
git commit -m "feat(frontend): LLM backend interface with Anthropic and MCP implementations"
```

---

## Task 13: Frontend — main CSS

**Files:**
- Create: `frontend/src/ui/main.css`

Minimal Anthropic-ish palette: warm neutral background, dark slate text, single rust accent. Mobile-first.

- [ ] **Step 1: Create `main.css`**

```css
:root {
  --bg: #f5f1e8;
  --card: #ffffff;
  --text: #2d2a26;
  --muted: #76706a;
  --accent: #c84e1a;
  --border: #e3ddd0;
  --danger: #b3261e;
  --radius: 12px;
}
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; background: var(--bg); color: var(--text); font: 16px/1.5 -apple-system, system-ui, "Segoe UI", sans-serif; }
.app { max-width: 640px; margin: 24px auto; padding: 0 16px; }
.card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); padding: 20px; }
.segmented { display: flex; gap: 4px; background: var(--border); padding: 4px; border-radius: var(--radius); margin-bottom: 16px; }
.segmented button { flex: 1; background: transparent; border: 0; padding: 8px 12px; border-radius: 8px; cursor: pointer; color: var(--muted); }
.segmented button.active { background: var(--card); color: var(--text); font-weight: 600; }
label { display: block; margin: 12px 0 4px; font-size: 14px; color: var(--muted); }
input, select, textarea { width: 100%; padding: 10px; border: 1px solid var(--border); border-radius: 8px; font: inherit; background: #fff; color: var(--text); }
textarea { min-height: 140px; resize: vertical; }
button.primary { background: var(--accent); color: #fff; border: 0; padding: 10px 16px; border-radius: 8px; font: inherit; cursor: pointer; }
button.primary:disabled { opacity: 0.5; cursor: not-allowed; }
.toast { position: fixed; bottom: 20px; left: 50%; transform: translateX(-50%); background: var(--text); color: #fff; padding: 10px 16px; border-radius: 8px; }
.toast.error { background: var(--danger); }
.result { margin-top: 12px; padding: 12px; background: #faf7f0; border-radius: 8px; border: 1px solid var(--border); }
.muted { color: var(--muted); font-size: 14px; }
a { color: var(--accent); }
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/ui/main.css
git commit -m "feat(frontend): main stylesheet with Anthropic-ish palette"
```

---

## Task 14: Frontend — `setup.js` page

**Files:**
- Create: `frontend/src/pages/setup.js`

Setup page asks for PAT, owner/repo, optional Anthropic key. Validates by calling `/user` and `/repos/:o/:r`. Seeds `INDEX.md`/`GENERAL.md`/`UNSURE.md` if repo is empty.

- [ ] **Step 1: Create `setup.js`**

```js
import { GitHubClient } from "../services/github.js";

const SEED_INDEX = `# INDEX for team-memory
GENERAL.md | shared | general
UNSURE.md | shared | unsure
`;

export function renderSetup(root, { onDone, config }) {
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
    </div>
  `;
  root.querySelector("#save").onclick = async () => {
    const status = root.querySelector("#status");
    const token = root.querySelector("#pat").value.trim();
    const repoStr = root.querySelector("#repo").value.trim();
    const anthropicKey = root.querySelector("#anthropic").value.trim();
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
      // ensure index/general/unsure exist
      const idx = await gh.getFile("INDEX.md");
      if (!idx.exists) {
        await gh.putContent({ path: "INDEX.md", content: SEED_INDEX, message: "team-memory: seed INDEX.md" });
        await gh.putContent({ path: "GENERAL.md", content: "# GENERAL\n", message: "team-memory: seed GENERAL.md" });
        await gh.putContent({ path: "UNSURE.md", content: "# UNSURE\n", message: "team-memory: seed UNSURE.md" });
        status.textContent = "Seeded fresh repo.";
      }
      const next = { token, owner, repo, anthropicKey, username: user.login };
      onDone(next);
    } catch (e) {
      status.textContent = "Error: " + e.message;
    }
  };
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/pages/setup.js
git commit -m "feat(frontend): setup page (PAT, repo, optional Anthropic key) with repo seeding"
```

---

## Task 15: Frontend — `remember.js` page

**Files:**
- Create: `frontend/src/pages/remember.js`

- [ ] **Step 1: Create `remember.js`**

```js
import { GitHubClient } from "../services/github.js";
import { Cache } from "../services/cache.js";
import { parseIndex, serializeIndex, upsertEntry } from "../services/indexmd.js";
import { renderEntry } from "../services/entries.js";
import { pickBackend } from "../services/llm/backend.js";

const TYPES = ["General", "Meeting Notes", "Unsure", "Programming", "Ideas", "Reminders"];

export function renderRemember(root, { config, toast }) {
  const gh = new GitHubClient({ token: config.token, owner: config.owner, repo: config.repo });
  const cache = new Cache("team-memory");

  root.innerHTML = `
    <div class="card">
      <h2>Remember</h2>
      <label>Memory text</label>
      <textarea id="text" placeholder="What do you want to remember?"></textarea>
      <label>Or upload a .txt file</label>
      <input id="file" type="file" accept=".txt,text/plain" />
      <label>Scope</label>
      <select id="scope"><option>Team</option><option>Personal</option></select>
      <label>Type</label>
      <select id="type">${TYPES.map(t => `<option>${t}</option>`).join("")}</select>
      <label>Target file <span class="muted">(leave blank to auto-pick or fall back to GENERAL.md / UNSURE.md)</span></label>
      <input id="target" type="text" placeholder="shared/programming-practices.md" />
      <div style="display:flex; gap:8px; margin-top:12px;">
        <button class="primary" id="auto">Auto-categorize</button>
        <button class="primary" id="save">Save</button>
      </div>
      <div id="preview"></div>
    </div>
  `;

  const $ = sel => root.querySelector(sel);

  $("#file").onchange = async (e) => {
    const f = e.target.files[0];
    if (!f) return;
    if (f.size > 1024 * 1024) { toast("File too large (max 1MB for slice 1)", true); return; }
    $("#text").value = await f.text();
  };

  $("#auto").onclick = async () => {
    try {
      const text = $("#text").value.trim();
      if (!text) { toast("Type or upload some text first.", true); return; }
      const backend = await pickBackend({ anthropicKey: config.anthropicKey, mcpUrl: "http://127.0.0.1:7438" });
      if (!backend) { toast("No LLM available. Configure an Anthropic key or start MCP.", true); return; }
      const indexFile = await getIndex(gh, cache);
      const scope = $("#scope").value === "Team" ? "Team" : `Personal:${config.username}`;
      const result = await backend.categorize({
        index: indexFile.content,
        payload: {
          scope, type: $("#type").value, text,
          source: "UI", timestamp: new Date().toISOString(),
        },
      });
      $("#target").value = result.target_file;
      $("#preview").innerHTML = `<div class="result"><b>${result.short_title}</b><br>${result.one_sentence_summary}<br>tags: ${result.tags}<br><small>${result.unsure ? "low confidence — saving to UNSURE.md" : ""}</small></div>`;
      $("#save").dataset.preset = JSON.stringify(result);
    } catch (e) {
      toast("Auto-categorize failed: " + e.message, true);
    }
  };

  $("#save").onclick = async () => {
    try {
      const text = $("#text").value.trim();
      if (!text) { toast("Empty memory.", true); return; }
      const type = $("#type").value;
      const scopeSel = $("#scope").value;
      const scope = scopeSel === "Team" ? "Team" : `Personal:${config.username}`;
      const preset = $("#save").dataset.preset ? JSON.parse($("#save").dataset.preset) : null;
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
      // If file is new to INDEX.md, add the line.
      const idxFile = await getIndex(gh, cache, /*forceFresh*/ true);
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
    }
  };
}

async function getIndex(gh, cache, forceFresh = false) {
  if (!forceFresh) {
    const cached = await cache.get("file:INDEX.md");
    if (cached) return cached;
  }
  const f = await gh.getFile("INDEX.md");
  await cache.set("file:INDEX.md", f);
  return f;
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/pages/remember.js
git commit -m "feat(frontend): Remember page with auto-categorize, manual fallback, and INDEX.md update"
```

---

## Task 16: Frontend — `lookup.js` page

**Files:**
- Create: `frontend/src/pages/lookup.js`

- [ ] **Step 1: Create `lookup.js`**

```js
import { GitHubClient } from "../services/github.js";
import { Cache } from "../services/cache.js";
import { parseIndex } from "../services/indexmd.js";

export function renderLookup(root, { config, toast }) {
  const gh = new GitHubClient({ token: config.token, owner: config.owner, repo: config.repo });
  const cache = new Cache("team-memory");

  root.innerHTML = `
    <div class="card">
      <h2>Lookup</h2>
      <label>Search</label>
      <input id="q" type="text" placeholder="keywords across INDEX and entry headers" />
      <div id="results" style="margin-top:12px;"></div>
    </div>
  `;
  const $ = s => root.querySelector(s);
  let timer;
  $("#q").oninput = () => {
    clearTimeout(timer);
    timer = setTimeout(() => doSearch($("#q").value, $("#results"), gh, cache, config, toast), 200);
  };
}

async function doSearch(query, out, gh, cache, config, toast) {
  query = query.trim().toLowerCase();
  if (!query) { out.innerHTML = ""; return; }
  try {
    let idxFile = await cache.get("file:INDEX.md");
    if (!idxFile) {
      idxFile = await gh.getFile("INDEX.md");
      await cache.set("file:INDEX.md", idxFile);
    }
    const idx = parseIndex(idxFile.content);
    const fileHits = idx.entries.filter(e =>
      e.path.toLowerCase().includes(query) ||
      e.topics.toLowerCase().includes(query)
    );
    // also fetch up to 5 candidate files and grep entry headers
    const entryHits = [];
    for (const e of fileHits.slice(0, 5)) {
      const cached = await cache.get("file:" + e.path) ?? await fetchAndCache(gh, cache, e.path);
      const lines = cached.content.split(/\r?\n/);
      lines.forEach((l, i) => {
        if (l.startsWith("### Entry:") && l.toLowerCase().includes(query)) {
          entryHits.push({ path: e.path, header: l, line: i });
        }
      });
    }
    out.innerHTML = renderResults(config, fileHits, entryHits);
  } catch (e) {
    toast("Lookup failed: " + e.message, true);
  }
}

async function fetchAndCache(gh, cache, path) {
  const f = await gh.getFile(path);
  await cache.set("file:" + path, f);
  return f;
}

function renderResults(config, fileHits, entryHits) {
  const url = (p, line) => `https://github.com/${config.owner}/${config.repo}/blob/master/${p}${line ? `#L${line + 1}` : ""}`;
  const fileList = fileHits.map(e => `<li><a href="${url(e.path)}" target="_blank">${e.path}</a> <span class="muted">${e.topics}</span></li>`).join("");
  const entryList = entryHits.map(h => `<li><a href="${url(h.path, h.line)}" target="_blank">${h.header}</a> <span class="muted">${h.path}</span></li>`).join("");
  return `
    <div class="result">
      <b>Files (${fileHits.length})</b>
      <ul>${fileList || "<li class='muted'>none</li>"}</ul>
      <b>Entries (${entryHits.length})</b>
      <ul>${entryList || "<li class='muted'>none</li>"}</ul>
    </div>
  `;
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/pages/lookup.js
git commit -m "feat(frontend): Lookup page with text search over INDEX and entry headers"
```

---

## Task 17: Frontend — app shell (`index.html` + `app.js`)

**Files:**
- Create: `frontend/index.html`
- Create: `frontend/src/app.js`

- [ ] **Step 1: Create `index.html`**

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>team-memory</title>
  <link rel="stylesheet" href="./src/ui/main.css" />
</head>
<body>
  <div class="app">
    <h1>team-memory</h1>
    <div class="segmented" id="nav">
      <button data-page="remember">Remember</button>
      <button data-page="lookup">Lookup</button>
      <button data-page="setup">Setup</button>
    </div>
    <div id="root"></div>
  </div>
  <script type="module" src="./src/app.js"></script>
</body>
</html>
```

- [ ] **Step 2: Create `app.js`**

```js
import { renderSetup } from "./pages/setup.js";
import { renderRemember } from "./pages/remember.js";
import { renderLookup } from "./pages/lookup.js";

const CONFIG_KEY = "team-memory:config";

function loadConfig() {
  try { return JSON.parse(localStorage.getItem(CONFIG_KEY)) || {}; } catch { return {}; }
}
function saveConfig(c) { localStorage.setItem(CONFIG_KEY, JSON.stringify(c)); }

function toast(msg, isError = false) {
  const t = document.createElement("div");
  t.className = "toast" + (isError ? " error" : "");
  t.textContent = msg;
  document.body.appendChild(t);
  setTimeout(() => t.remove(), 3500);
}

const root = document.getElementById("root");
const nav = document.getElementById("nav");

function go(page) {
  const config = loadConfig();
  if (page !== "setup" && (!config.token || !config.owner)) {
    page = "setup";
  }
  for (const b of nav.querySelectorAll("button")) {
    b.classList.toggle("active", b.dataset.page === page);
  }
  if (page === "setup") {
    renderSetup(root, {
      config,
      onDone: c => { saveConfig(c); toast("Setup saved."); go("remember"); },
    });
  } else if (page === "remember") {
    renderRemember(root, { config, toast });
  } else {
    renderLookup(root, { config, toast });
  }
}

nav.addEventListener("click", e => {
  const b = e.target.closest("button[data-page]");
  if (b) go(b.dataset.page);
});

go("remember");
```

- [ ] **Step 3: Manual smoke**

```bash
make prompts
npm run serve
# open http://localhost:8080 in browser
# - Setup page should show
# - Enter PAT + repo + optionally an Anthropic key
# - Save & verify; expect "Authenticated as <login>"
# - Switch to Remember, type "test memory", click Save
# - Open the repo on GitHub; confirm GENERAL.md has a new ### Entry block
# - Switch to Lookup, type "test"; expect a hit
```

- [ ] **Step 4: Commit**

```bash
git add frontend/index.html frontend/src/app.js
git commit -m "feat(frontend): app shell with router, nav, and toast"
```

---

## Task 18: README + smoke checklist

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Replace `README.md`**

````markdown
# team-memory

Local-first memory app. All data lives in your own GitHub repo as Markdown.
Slice 1 ships a static web UI plus an optional local Go MCP (`team-memory-mcp`)
that wraps the `claude` CLI so you can use the app without an Anthropic API key.

See `docs/superpowers/specs/2026-05-02-team-memory-slice-1-design.md` for the
full design and `ROADMAP.md` for what's coming next.

## Prereqs

- Node 20+ (only for `npm test` and `python -m http.server`; no build step)
- Go 1.22+ (only if you want the local MCP)
- `claude` CLI on PATH (only if MCP needs to call the LLM with no API key)
- A GitHub repo and a fine-grained PAT with `contents:write` on that repo

## Quickstart (browser-only, with Anthropic key)

```bash
make prompts
npm run serve
# open http://localhost:8080
# Setup page: paste PAT, owner/repo, Anthropic API key
```

## Quickstart (browser + local MCP, no Anthropic key)

```bash
make prompts
make mcp
./mcp/team-memory-mcp &
npm run serve
# open http://localhost:8080
# Setup page: paste PAT, owner/repo; leave Anthropic key blank
```

## Tests

```bash
make test           # runs frontend + MCP tests
```

## Smoke checklist

1. Setup page accepts PAT + repo, shows "Authenticated as <login>".
2. If repo was empty, INDEX.md / GENERAL.md / UNSURE.md appear in GitHub.
3. Remember page → type "test memory" → Save → entry appears in GENERAL.md
   on GitHub with the right `### Entry:` block.
4. Auto-categorize button (with Anthropic key OR MCP running) returns a
   target file and summary; saving commits to that file and updates INDEX.md.
5. Lookup page finds the new entry by keyword.
6. Pulling the repo from GitHub directly while save is in flight should
   produce a "file changed in GitHub" error after 3 retries (manual test).

## Privacy

- PAT and Anthropic key are stored in your browser's `localStorage`. Treat the
  browser profile as the trust boundary. (Web Crypto encryption is on the
  ROADMAP.)
- The MCP binds `127.0.0.1` only.
- No telemetry, no third-party scripts.
````

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: README with quickstart, smoke checklist, and privacy notes"
```

---

## Self-review notes

**Spec coverage:**
- Setup / PAT / username detection → Task 14
- Remember (manual + auto) → Task 15
- Lookup (text search) → Task 16
- IndexedDB cache → Task 11
- INDEX.md parse/update → Task 8
- Entry template → Task 9
- GitHub SHA-retry commit + conflict modal classification → Task 10 + Task 15 (toast handles modal-equivalent UX for slice 1)
- LLM backend abstraction → Task 12
- MCP `/health` + `/v1/categorize` + `/v1/summarize` + stream-json + `claude` subprocess → Tasks 3–7
- Embedded prompts (single source of truth) → Task 2 + 4
- README + smoke checklist → Task 18

**Type/name consistency check:** verified `commitFile`, `putContent`, `getFile`, `getUser` used consistently in `github.js` and `setup.js`/`remember.js`/`lookup.js`. Entry shape (`shortTitle`, `bullets`, etc.) matches between `entries.js` test and `remember.js` consumer.

**Known limitation accepted by design:** Conflict UX in slice 1 is a toast, not a modal with [Open in GitHub] / [Discard] / [Try again] buttons as described in the spec. This is a deliberate slice 1 simplification — bumping it to a real modal is a small follow-up but doesn't change behavior. Captured here as a one-task follow-up if user wants it before slice 1 is "done".
