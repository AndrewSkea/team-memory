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

## Claude Code Hook Setup

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
