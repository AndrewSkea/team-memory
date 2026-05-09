# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

`team-memory` is a local-first developer knowledge base. A Go binary (`team-memory-mcp`) serves three roles depending on flags:

- `--port N` — HTTP server + embedded web UI at `http://127.0.0.1:7438`
- `--mcp` — MCP stdio server for Claude Code (exposes `remember` and `lookup` tools)
- `--once session-end|precompact` — one-shot hook handler; reads a Claude Code session transcript from stdin, summarises it via `claude` CLI, and commits structured Markdown to the user's GitHub memory repo

## Commands

```bash
make build          # copy prompts → frontend/prompts/ + mcp/prompts/, copy frontend → mcp/frontend/, build binary
make install        # build + copy binary to ~/bin
make test           # frontend unit tests + Go unit tests
make test-mcp       # Go tests only: cd mcp && go test ./...
make test-frontend  # Node tests only: node --test tests/frontend
make test-e2e       # Playwright e2e (requires built binary first: make build)
make test-all       # all of the above
```

Run a single Go test:
```bash
cd mcp && go test ./hook/ -run TestRenderEntry
```

Run a single frontend test file:
```bash
node --test tests/frontend/entries.test.js
```

Preview frontend changes:
```bash
make build && ./mcp/team-memory-mcp --port 7438
```

## Architecture

### Build-time copy step is critical

`prompts/*.txt` is the **source of truth** for all LLM prompts. The build step (`scripts/copy-prompts.sh`, called by `make build` and `goreleaser`) copies them into two places:

- `frontend/prompts/` — served as static files, fetched by the browser at runtime
- `mcp/prompts/` — embedded into the Go binary via `go:embed` in `mcp/prompts/embed.go`

Additionally, the entire `frontend/` directory is copied into `mcp/frontend/` during this step, which is what gets embedded into the binary (`mcp/embed.go`). **`mcp/frontend/` and `mcp/prompts/*.txt` (other than `.gitkeep`) are build artifacts — do not edit them directly.**

### Entry format must stay in sync

The Markdown entry format is implemented in two places that must stay identical:

- `mcp/hook/render.go` — Go, used by the hook handler and MCP `remember` tool
- `frontend/src/services/entries.js` — JS, used by the web UI

Both produce entries starting with `### Entry: <RFC3339> — <title>` followed by `**Scope:**`, `**Type:**`, `**Tags:**`, `**Source:**`, `**Summary:**`, `**Bullets:**`, and `**Full:**` fields.

### LLM invocation

**Go binary:** `mcp/llm/claude.go` shells out to the `claude` CLI with flags `-p <prompt> --output-format stream-json --verbose`. `mcp/llm/stream.go` parses the streaming JSONL output to extract the assistant text.

**Frontend:** `frontend/src/services/llm/backend.js` picks a backend at runtime: direct Anthropic API (`anthropic.js`) if an API key is configured in `localStorage`, otherwise the local MCP HTTP server's `/v1/categorize` and `/v1/summarize` endpoints.

### GitHub storage

`mcp/github/client.go` talks to the GitHub Contents API (no external dependencies). The user's memory repo is expected to contain:

- `INDEX.md` — table of contents listing memory files (`filename.md | scope | type` per line)
- `*.md` files — memory files, each containing zero or more `### Entry:` blocks

`CommitFile` appends content and retries up to 3 times on HTTP 409 (concurrent write conflicts). `PutContent` overwrites without retry (used for `INDEX.md` updates).

### Hook flow (`--once`)

1. Reads JSON `{transcript: [{role, content}]}` from stdin
2. Truncates transcript to last 80 messages
3. Fetches `INDEX.md` from GitHub
4. Runs the `session.txt` prompt via `claude` CLI → gets back JSON with `target_file`, `short_title`, `one_sentence_summary`, `bullets`, `tags`, `unsure`
5. If `unsure: true` or no target file, writes to `UNSURE.md`
6. Appends rendered entry to the target file
7. Updates `INDEX.md` if the file wasn't already listed

### HTTP API routes

| Route | Method | Purpose |
|---|---|---|
| `/health` | GET | Version + claude CLI availability |
| `/v1/config` | GET | Read CLI config from disk |
| `/v1/export-config` | POST | Write CLI config to disk |
| `/v1/categorize` | POST | Run categorize prompt via claude CLI |
| `/v1/summarize` | POST | Run summarize prompt via claude CLI |

All `/v1/` routes have CORS enabled for localhost origins (`mcp/server/cors.go`).

## Code conventions

- **Go:** standard `gofmt`, no external dependencies beyond stdlib, interfaces (`Runner`, `GitHubClient`) are defined in `hook/handler.go` for testability
- **JS:** vanilla ES modules, no bundler, no framework; frontend config lives in `localStorage` under key `team-memory:config`
- **Releases:** push a `v*` tag to trigger goreleaser via GitHub Actions; the `version` string is injected via ldflags
