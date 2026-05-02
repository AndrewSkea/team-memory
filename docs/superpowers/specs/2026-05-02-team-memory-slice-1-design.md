# team-memory — Slice 1 Design

**Date:** 2026-05-02
**Status:** Approved
**Scope:** First implementable slice. All deferred scope is in `ROADMAP.md`.

## Goal

A local-first memory app that lets a single user save and look up Markdown
memories in their own GitHub repo. Slice 1 ships a static web frontend plus a
minimal local Go MCP that wraps `claude -p` so the app is usable without an
Anthropic API key.

## Non-goals (slice 1)

OAuth, PDF/DOCX uploads, Forget/Stale/Restructure, LLM-assisted Lookup, real
`git pull --rebase`/PR conflict handling, MCP-side local clone, MCP commits,
OS keychain, encrypted credential storage, CLI shim, Claude Code session
hooks, prebuilt cross-platform binaries, full E2E tests. All captured in
`ROADMAP.md`.

## Architecture

```
┌──────────────────────────────┐         ┌──────────────────────┐
│  Browser (static site)       │ ──────► │  GitHub REST API     │
│                              │         └──────────────────────┘
│  ┌────────────────────────┐  │         ┌──────────────────────┐
│  │ UI (Remember/Lookup)   │  │ ──────► │  Anthropic API       │
│  ├────────────────────────┤  │         │  (if browser key)    │
│  │ GitHubClient           │  │         └──────────────────────┘
│  │ IndexedDB cache        │  │
│  │ LLMBackend interface   │  │         ┌──────────────────────┐
│  │   ├ AnthropicDirect    │  │         │  MCP (Go, loopback)  │
│  │   └ LocalMCP ──────────┼──┼──────►  │  /v1/categorize      │
│  └────────────────────────┘  │         │  /v1/summarize       │
└──────────────────────────────┘         │  /health             │
                                         │       ▼              │
                                         │  claude -p           │
                                         │  (subprocess,        │
                                         │   stream-json)       │
                                         └──────────────────────┘
```

**Boundaries:**
- **Frontend** owns: UI, GitHub commits, IndexedDB cache, LLM dispatch,
  `INDEX.md` parse/update, entry rendering.
- **MCP** owns: nothing about GitHub, nothing about caching. It is a
  single-purpose LLM-call wrapper that takes a prompt payload and returns the
  JSON the prompt requested.
- **Prompts** live as `.txt` files in `prompts/` at repo root, copied into
  `frontend/prompts/` at build time and embedded into the MCP binary via
  `//go:embed`.

## Module layout

### Frontend (vanilla ES modules, no framework)

```
frontend/
  index.html
  src/
    app.js            # router, page mount
    pages/
      setup.js        # PAT, repo, optional Anthropic key entry
      remember.js
      lookup.js
    services/
      github.js       # REST client, SHA-retry commit
      cache.js        # IndexedDB wrapper
      indexmd.js      # parse + serialize INDEX.md
      entries.js      # entry template render + append
      llm/
        backend.js    # interface
        anthropic.js  # browser → api.anthropic.com
        mcp.js        # browser → http://localhost:<port>
    ui/
      card.css
      segmented.css
  prompts/            # copied from /prompts at build
```

### MCP (Go)

```
mcp/
  main.go             # flag parsing, server bootstrap
  server/
    server.go         # http.ServeMux, loopback bind (127.0.0.1)
    handlers.go       # /health, /v1/categorize, /v1/summarize
  llm/
    claude.go         # subprocess: claude -p --output-format stream-json --verbose
    stream.go         # consume + reassemble stream-json into final JSON
  prompts/
    embed.go          # //go:embed *.txt
```

### Repo root

```
prompts/              # source of truth for prompt .txt files
docs/superpowers/specs/
ROADMAP.md
README.md
```

## Setup flow

1. User opens the static site (served via `python -m http.server` or
   `npx serve` for slice 1; production hosting is a later concern).
2. Setup page asks for:
   - GitHub PAT (fine-grained, scoped to one repo, `contents:write`)
   - Repo (owner/name)
   - Optional Anthropic API key
3. UI calls `GET /user` to capture GitHub login (used as `<name>` for personal
   files and entry attribution).
4. UI calls `GET /repos/:o/:r` to verify access.
5. If repo is empty, UI seeds `INDEX.md`, `GENERAL.md`, `UNSURE.md` with empty
   content.
6. Credentials stored in `localStorage` (no Web Crypto in slice 1; documented
   limit). Username, repo, and the chosen LLM backend are also persisted.

## Remember flow

**With Auto-categorize ON and an LLM backend available:**

1. User types text, picks Scope (Team/Personal) and Type (General, Meeting
   Notes, Unsure, Programming, Ideas, Reminders).
2. Click "Auto-categorize" → UI loads `INDEX.md` (cache or fresh fetch).
3. UI calls `LLMBackend.categorize({index, payload})`. Backend is whichever
   one is configured (`AnthropicDirect` if key present, else `LocalMCP`).
4. LLM returns JSON `{target_file, short_title, one_sentence_summary,
   bullets, tags, entry_header, unsure, rationale?}`.
5. UI shows result in editable preview card. User can change target file,
   edit summary/bullets, or save as-is.
6. On Save: `commitFlow(target_file, renderedEntry)` (see Commit flow below).
7. If file is new (didn't exist before this commit): also update `INDEX.md`.
8. Invalidate cache for both files. Toast "Saved to `<path>`" with link.

**With Auto-categorize OFF or no LLM available:**

1. User picks target file from a dropdown built from `INDEX.md`, or types a
   new path.
2. UI synthesizes the entry locally from the entry template; `short_title` =
   first line of text; bullets empty.
3. Same `commitFlow`.

**Default fallbacks:** Type=`Unsure` or blank target → `UNSURE.md`. Otherwise
blank → `GENERAL.md`.

## Commit flow (SHA-retry)

```
function commit(path, newContent):
  for attempt in 1..3:
    file = GET /repos/:o/:r/contents/<path>   # 404 → no sha, content=""
    merged = appendEntry(file.content, newEntry)
    res = PUT /repos/:o/:r/contents/<path> { content: merged, sha: file.sha }
    if res.ok: return success
    if res.status == 409: continue
    return error(res)
  return conflict_modal()
```

After 3 failed retries → modal: "Couldn't save — the file changed in GitHub
while you were editing. [Open in GitHub] [Discard] [Try again]". No silent
data loss.

## Lookup flow

1. User types query.
2. UI loads cached `INDEX.md` plus an in-memory inverted index over entry
   headers from cached files.
3. Score: case-insensitive substring match on `INDEX.md` lines (file-level
   hit) plus matches on `### Entry: ...` headers (entry-level hit). Simple
   tokenization.
4. Render top N: file path, entry header, snippet, "Open in GitHub" link.
   Clicking a file lazily fetches it if not cached.

## INDEX.md format

One line per file:

```
path | scope | primary_topics ; secondary_topics ; tags
```

Slice 1 generates new INDEX lines from the LLM's `target_file` and `tags`
fields when a new file is created. Manual mode appends an empty-tags line.

## Entry template

```markdown
### Entry: <ISO8601> — <short_title>
**Scope:** <Team|Personal:<name>>
**Type:** <type>
**Tags:** <a;b;c>
**Source:** <UI|CLI|upload:filename>
**Summary:** <one sentence>
**Bullets:** - <b1> ; - <b2> ; - <b3>
**Full:**
<full text>
```

## MCP API

All endpoints loopback-only (`127.0.0.1`). CORS allowlist:
`http://localhost:*` and `file://`.

- `GET /health` → `{"status":"ok","claude":"available|missing"}`
- `POST /v1/categorize`
  Body: `{"index":"<INDEX_CONTENT>","payload":{...},"token_budget":200}`
  Response: Categorize JSON schema (see prompts).
- `POST /v1/summarize`
  Body: `{"filename":"<n>","text":"<extracted>"}`
  Response: Summarize JSON schema.

**Backend:** subprocess `claude -p "<prompt>" --output-format stream-json
--verbose`. MCP consumes the stream and assembles the final assistant message,
then parses it as JSON. If parsing fails, returns 502 with the raw text.

**Errors:**
- `claude` not on PATH → 503 with install hint.
- `claude` exits non-zero → 502 with stderr tail.
- Response not valid JSON matching schema → 502 with raw text.

## Frontend `LLMBackend` interface

```js
interface LLMBackend {
  categorize({ index, payload }): Promise<CategorizeResult>
  summarize({ filename, text }): Promise<SummarizeResult>
  health(): Promise<{ available: boolean, detail?: string }>
}
```

Two implementations:
- `AnthropicDirect` — uses pasted browser key, calls `api.anthropic.com`.
- `LocalMCP` — calls `http://127.0.0.1:<port>/v1/...`.

Selection rule: Anthropic key set → `AnthropicDirect`. Else probe MCP
`/health` → if reachable, `LocalMCP`. Else "no LLM available" (manual mode
only).

## Error handling

- **GitHub:** classify as `auth` (401/403), `not-found`, `conflict` (409 →
  retry), `rate-limit` (403 + zero remaining → show reset time, disable
  saves), `other`.
- **LLM:** invalid key, MCP unreachable, schema-mismatched response → all
  surfaced clearly with actionable text. On schema mismatch, show raw
  response and offer manual save.
- **No swallowing:** every catch either recovers or surfaces to the user.

## Security posture (slice 1)

- PAT and Anthropic key in `localStorage`. No Web Crypto in slice 1;
  documented in setup as "treat this browser profile as the trust boundary."
- MCP binds `127.0.0.1` only. No `--host` flag.
- MCP has no auth (loopback assumption). Documented limit; loopback token in
  ROADMAP.
- Prompt injection from entries → accepted risk for single-user slice 1;
  mitigations in ROADMAP.
- Zero third-party scripts, no telemetry.

## Tests (slice 1, deliberately minimal)

**Frontend (`node --test`, zero deps):**
- `indexmd.js` parse/serialize round-trip
- `entries.js` template render
- `github.js` SHA-retry logic with mocked `fetch`
- `cache.js` set/get/invalidate

**MCP (`go test`):**
- prompt loading
- stream-json reassembly with fixture
- handler input validation
- one integration test that spawns a fake `claude` shell script printing a
  fixture and verifies `/v1/categorize` returns expected JSON (skipped on
  Windows in CI; documented)

**No browser+GitHub+MCP E2E** in slice 1 — manual smoke checklist in
`README.md`.

## Open follow-ups (out of slice 1)

See `ROADMAP.md`.
