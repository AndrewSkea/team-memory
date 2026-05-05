# team-memory Slice 2 Design — Claude Code Integration

**Date:** 2026-05-05
**Status:** Approved
**Builds on:** Slice 1 (static frontend + Go MCP)

---

## Goal

Make memory capture automatic. When a Claude Code session ends (or context compacts), team-memory saves a structured summary to the GitHub repo with no user action required. The user configures once via the web UI; everything else is silent.

---

## Scope

**In:**
- `team-memory-mcp --once session-end` one-shot command
- `team-memory-mcp --once precompact` one-shot command (same behaviour, different source label)
- Config file at `os.UserConfigDir()/team-memory/config.json`
- New `POST /v1/export-config` MCP endpoint (web UI → CLI config bridge)
- New `prompts/session.txt` system prompt for session summarization
- Setup page: "Check first" toggle + "Export to CLI config" button
- README: hook wiring instructions

**Out:**
- SessionStart hook (no local git clone in slice 2; a no-op is not worth shipping)
- Interactive "check first" confirmation in hook context (no TTY available)
- Local git clone / push (slice 3)
- OAuth Device Flow (slice 3)
- CLI subcommands beyond `--once` (slice 3)

---

## Architecture

```
Claude Code session ends
        │
        ▼
Stop hook fires → team-memory-mcp --once session-end
        │
        ├─ reads stdin JSON  (Claude Code hook event: {session_id, transcript:[...]})
        ├─ reads ~/.config/team-memory/config.json
        ├─ fetches INDEX.md from GitHub REST
        ├─ calls claude -p with session.txt prompt + transcript + INDEX
        │         └─ returns categorize JSON: {target_file, short_title, ...}
        ├─ builds markdown entry block (same template as frontend entries.js)
        ├─ appends entry to target_file via GitHub REST (SHA-retry, 3 attempts)
        └─ if target_file new: updates INDEX.md via GitHub REST
```

PreCompact hook is identical; `source` field in the entry is set to `PreCompact` instead of `Stop`.

---

## Config File

**Location:** `os.UserConfigDir() + "/team-memory/config.json"`
- Windows: `%APPDATA%\team-memory\config.json`
- macOS: `~/Library/Application Support/team-memory/config.json`
- Linux: `~/.config/team-memory/config.json`

**Schema:**
```json
{
  "token": "github_pat_...",
  "owner": "AndrewSkea",
  "repo": "my-knowledge",
  "check_first": false
}
```

`check_first` controls browser Remember page behaviour only (hooks always auto-save — no TTY for confirmation in hook context).

---

## One-Shot Mode (`--once`)

`main.go` detects `--once <command>` flag. If present, runs the command and exits instead of starting the HTTP server.

```
team-memory-mcp --once session-end    # Stop hook
team-memory-mcp --once precompact     # PreCompact hook
```

Both commands:
1. Read hook event JSON from stdin. Extract `transcript` array (role/content pairs). If transcript is empty or missing, exit 0 silently (nothing to save).
2. Truncate transcript to last 80 messages to stay within claude context.
3. Load config from disk. Exit 1 with actionable message if missing.
4. Fetch `INDEX.md` from GitHub. Use empty string if 404.
5. Build session prompt: `session.txt` system prompt + transcript text + INDEX content as user message.
6. Shell out to `claude -p "<prompt>" --output-format stream-json --verbose` (same runner as server mode). Strip markdown fences from output.
7. Parse JSON result (same schema as `/v1/categorize` output).
8. Build entry block using Go port of `renderEntry` from frontend.
9. Append to `target_file` via GitHub REST with SHA-retry (3 attempts on 409).
10. If `target_file` absent from INDEX.md: upsert line and PUT INDEX.md.
11. Exit 0 on success, exit 1 on any unrecoverable error (Claude Code logs hook failures).

---

## Session Prompt (`prompts/session.txt`)

System prompt that instructs claude to:
- Read a Claude Code session transcript (role/content pairs)
- Identify the primary topic, decisions made, and things learned
- Output **the same JSON schema as `/v1/categorize`**: `target_file`, `short_title`, `one_sentence_summary`, `bullets` (array), `tags` (semicolon-separated), `unsure` (bool)
- Pick `target_file` using the INDEX content provided, following existing naming conventions
- Set `unsure: true` if the session was trivial or ambiguous (resulting in save to UNSURE.md)
- Source field is not in the LLM output; the binary sets it to `"Stop"` or `"PreCompact"`

Reuses the categorize JSON schema so no new parsing code is needed.

---

## New MCP Endpoint: `POST /v1/export-config`

**Purpose:** Browser Setup page writes the CLI config file via the running MCP server. Avoids the user having to find and edit the file manually.

**Request:**
```json
{
  "token": "github_pat_...",
  "owner": "AndrewSkea",
  "repo": "my-knowledge",
  "check_first": false
}
```

**Response (200):**
```json
{ "ok": true, "path": "/Users/andrew/Library/Application Support/team-memory/config.json" }
```

**Response (500):** `{ "error": "..." }`

Creates parent directories if needed. Writes with 0600 permissions.

---

## Go GitHub Client (`mcp/github/`)

Mirrors the frontend `github.js`. Used by one-shot commands (server mode still uses claude subprocess only; no GitHub calls server-side today).

Functions:
- `GetFile(ctx, cfg, path) → {sha, content, exists}`
- `CommitFile(ctx, cfg, path, append, message) → error` (SHA-retry, 3 attempts)
- `PutContent(ctx, cfg, path, content, message) → error` (SHA-retry, 3 attempts)

Uses `net/http` with Bearer token. b64 encode/decode via `encoding/base64`.

---

## Frontend Changes (Setup Page)

Two additions below the existing "Save & verify" flow:

**1. "Check first" toggle**
```
[ ] Check first before saving (browser Remember page only)
```
Stored in localStorage config as `check_first: true/false`. When true, the Remember page shows a confirmation step before committing manual saves (saves without auto-categorize currently commit immediately).

**2. "Export to CLI config" button**
- Only shown after setup is verified (token + repo confirmed).
- POSTs to `http://127.0.0.1:7438/v1/export-config` with current config.
- Shows: `✓ Config saved to <path>` or `✗ MCP not running — start it first`.

---

## Remember Page: Check First behaviour

When `config.check_first === true` and user clicks Save **without** having run Auto-categorize first:
- Show a confirmation card: "Save this text to GENERAL.md as an unstructured entry? [Save] [Cancel]"
- When Auto-categorize has already run (preset is set), skip confirmation (user already reviewed the preview).

---

## Go Package Layout

```
mcp/
  config/
    config.go         # read/write config file; UserConfigDir resolution
    config_test.go
  github/
    client.go         # GetFile, CommitFile, PutContent via REST
    client_test.go    # mock http.Client tests
  hook/
    handler.go        # RunSessionEnd(cfg, transcript) and RunPreCompact — same logic
    render.go         # Go port of renderEntry (markdown block builder)
    handler_test.go   # fake claude runner, fake github client
  prompts/
    embed.go          # updated: adds session.txt
    session.txt       # new
  server/
    handlers.go       # new /v1/export-config handler
  main.go             # --once flag detection and dispatch
```

---

## Claude Code Hook Configuration

Users add to `~/.claude/settings.json` (global) or `.claude/settings.json` (project):

```json
{
  "hooks": {
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "team-memory-mcp --once session-end"
          }
        ]
      }
    ],
    "PreCompact": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "team-memory-mcp --once precompact"
          }
        ]
      }
    ]
  }
}
```

`team-memory-mcp` must be on PATH. `make install` copies it to `~/bin/` (or `/usr/local/bin/` with sudo).

---

## Makefile Additions

```makefile
install:
    cd mcp && go build -o team-memory-mcp$(EXT) .
    mkdir -p ~/bin
    cp mcp/team-memory-mcp$(EXT) ~/bin/
    @echo "Installed to ~/bin/team-memory-mcp$(EXT)"
```

Where `EXT` is `.exe` on Windows (detected via `GOOS`).

---

## Tests

**Go unit tests:**
- `config`: read/write round-trip, missing file returns typed error, 0600 permissions
- `github`: GetFile 404 returns `exists:false`, CommitFile retries on 409, PutContent success
- `hook`: given fake transcript + fake runner output → verify entry markdown shape; given empty transcript → exits silently; given claude error → returns error

**Frontend:**
- No new test files (export-config is a thin HTTP call; check_first toggle is UI-only)
- `npm test` must continue to pass (16 tests)

---

## Entry Block Format

Same as slice 1. `source` field is `"Stop"` or `"PreCompact"` (set by binary, not LLM).

```markdown
### Entry: 2026-05-05T14:23:00Z — <short_title>
**Scope:** Team
**Type:** <type from categorize>
**Tags:** <tags>
**Source:** Stop
**Summary:** <one_sentence_summary>
**Bullets:** - <b1> ; - <b2> ; - <b3>
**Full:**
<one_sentence_summary>
```

`Full` is set to the one-sentence summary (the raw transcript is not stored; too large).

---

## Error Handling

| Condition | Behaviour |
|---|---|
| Config file missing | Exit 1: "team-memory: no config found — open the web UI, complete Setup, click 'Export to CLI config'" |
| `claude` not on PATH | Exit 1: "team-memory: claude CLI not found — install from claude.ai/cli" |
| Claude returns non-JSON | Exit 1 with raw output truncated to 300 chars |
| GitHub 401/403 | Exit 1: "team-memory: GitHub auth failed — check PAT in web UI Setup" |
| GitHub 409 × 3 | Exit 1: "team-memory: conflict writing <path> after 3 retries" |
| Empty transcript | Exit 0 silently (nothing to save) |

Exit 1 causes Claude Code to log a hook failure warning. It does not block the session from ending.

---

## Security

- Config file written with 0600 (owner-read-only). On Windows, ACL inherits from `%APPDATA%` which is user-private.
- PAT is never logged or printed to stdout/stderr in normal operation (only in debug mode if added later).
- `/v1/export-config` is loopback-only (same CORS rules as existing MCP endpoints).
- Hook commands run with no elevated privileges.
