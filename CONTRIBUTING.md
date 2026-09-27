# Contributing to team-memory

## Prerequisites

- Go (version in `mcp/go.mod`)
- Node 22+ (web UI tests only)

No GitHub account, token or Claude login is needed to run the tests: they use an in-memory fake of the GitHub API and a scripted stand-in for the LLM.

## Build and test

```bash
make build      # embed prompts + web UI, build mcp/team-memory-mcp
make test       # Go tests (incl. a full end-to-end run of the real binary) + web UI unit tests
make test-e2e   # Playwright browser tests against the real binary and a fake GitHub
```

Run a local instance against the fake GitHub:

```bash
make build fakegh
./mcp/fakegh &                                    # fake acme/kb, token "tok", user "alice"
TEAM_MEMORY_API_URL=http://127.0.0.1:19439 \
TEAM_MEMORY_CONFIG=/tmp/tm/config.json TEAM_MEMORY_DATA=/tmp/tm/data \
  ./mcp/team-memory-mcp serve                     # open http://127.0.0.1:7438, repo acme/kb, token tok
```

## Layout

```
mcp/                     Go module — the single team-memory-mcp binary
  main.go, cli_*.go      CLI commands (serve, mcp, hook, install, doctor, …)
  app/                   Core: capture pipeline, recall, review queue, setup, stats
  hook/                  Claude Code hook handlers (+ detached capture worker)
  mcpserver/             MCP stdio server and tools
  server/                Local HTTP API for the web UI
  store/                 Local mirror of the memory repo (tarball sync, write-through)
  memory/                Memory file format (frontmatter) + legacy parser
  search/                BM25 + entity ranking
  entities/              System catalogue and alias matching
  policy/                Capture modes and team policy
  secrets/               Credential redaction
  workspace/             Git remote and dependency/env signals for a directory
  transcript/            Claude Code transcript reader
  github/                GitHub REST client
  installer/             Claude Code settings, MCP registration, slash commands, launchd/systemd
  llm/                   claude CLI / Anthropic API runners
  internal/fakegh        Fake GitHub API for tests (also cmd/fakegh)
frontend/                Vanilla JS web UI, embedded in the binary
prompts/                 LLM prompts (source of truth; copied into mcp/prompts by make)
install.sh, install.ps1  Download + verify + `team-memory-mcp install`
tests/frontend, tests/e2e
```

## Environment variables

| Variable | Purpose |
|---|---|
| `TEAM_MEMORY_CONFIG` / `TEAM_MEMORY_DATA` | Override config file / data directory |
| `TEAM_MEMORY_API_URL` | GitHub API base (tests, GitHub Enterprise) |
| `TEAM_MEMORY_MODEL` | Model alias for the `claude` CLI (default `haiku`) |
| `TEAM_MEMORY_CLAUDE` | Path to the `claude` binary |
| `ANTHROPIC_API_KEY` | Use the Messages API when the `claude` CLI isn't installed |

## Guidelines

- Keep the binary self-contained and the dependency list short.
- Hooks must never break a Claude Code session: log and exit 0.
- Anything that reaches the memory repo goes through `secrets.Redact` and the capture policy.
- Go: `gofmt`, `go vet`. JS: vanilla ES modules, escape everything you interpolate into HTML.
- Add tests with behaviour changes; CI runs on macOS, Linux and Windows.

## Releases

Push a `v*` tag. The release workflow runs the tests, then GoReleaser publishes binaries and checksums that the installers download.
