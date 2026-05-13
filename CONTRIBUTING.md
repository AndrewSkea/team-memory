# Contributing to team-memory

## Prerequisites

- Go 1.22+
- Node 20+
- A GitHub repo + fine-grained PAT with `contents:write` (for manual testing)

## Build

```bash
make build          # copies prompts, builds binary → mcp/team-memory-mcp(.exe)
make install        # build + copy to ~/bin
make test           # frontend + MCP unit tests
make test-all       # + Playwright e2e tests
```

## Project layout

```
mcp/                Go binary (MCP server + HTTP server + hooks)
  config/           Config file read/write
  github/           GitHub REST client (get, commit, put)
  hook/             Session-end / precompact hook handlers
  llm/              Claude CLI runner
  server/           HTTP handlers (/health, /v1/config, /v1/categorize, …)
frontend/           Vanilla JS + CSS web UI (embedded into binary via go:embed)
  src/pages/        Page renderers (remember, lookup, stats, stale, setup)
  src/services/     GitHub client, cache, LLM backends, index parser
prompts/            Source-of-truth prompt text files
scripts/            copy-prompts.sh (prompts/ → frontend/prompts/ + mcp/prompts/)
install.sh          Bash installer (macOS/Linux)
install.ps1         PowerShell installer (Windows)
```

The binary serves two roles depending on flags:
- `--port N` — HTTP server + embedded web UI
- `--mcp` — MCP stdio server (used by Claude Code)
- `--once session-end|precompact` — one-shot hook handler

## Making changes

1. Fork and create a branch.
2. Run `make test` before pushing.
3. For frontend changes, run `make build` then `./mcp/team-memory-mcp --port 7438` to preview.
4. Open a PR against `master`.

## Code style

- Go: standard `gofmt`, no external linter required
- JS: vanilla ES modules, no bundler, no framework
- Keep dependencies minimal — the binary must stay a single self-contained file

## Releases

Push a `v*` tag to trigger goreleaser via GitHub Actions. Binaries are published automatically.
