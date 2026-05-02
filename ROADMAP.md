# team-memory — Roadmap

This roadmap captures the full product vision. Slice 1 (active) is specced in
`docs/superpowers/specs/2026-05-02-team-memory-slice-1-design.md`. Everything
below is **deferred** and will be brainstormed/specced as its own slice when we
get to it.

## Slice 1 — Static UI + GitHub + minimal MCP (active)
Static frontend with PAT auth, Remember + Lookup pages, IndexedDB cache,
GitHub REST commits with SHA-retry, minimal Go MCP that shells out to
`claude -p --output-format stream-json --verbose`. Browser `LLMBackend`
abstraction with `AnthropicDirect` and `LocalMCP` implementations.

## Deferred slices

### Auth & credentials
- GitHub OAuth Device Flow (alternative to PAT)
- Web Crypto passphrase encryption for PAT and Anthropic key in browser
- OS keychain integration for MCP credentials
- Encrypted credential file fallback (`~/.claude/team_mem_credentials.json`)
- Loopback auth token between browser and MCP

### Uploads & extraction
- PDF extraction in browser (pdf.js)
- DOCX extraction in browser (mammoth.js)
- Summarize-uploaded-document prompt + flow
- File size guardrails (5MB cap)

### Forget flow
- Natural-language Forget page UI
- Forget prompt + JSON match output
- Confirmation UX with confidence display
- Append deletion record to `FORGET.md`; redact/remove entries

### Stale flow
- Stale page listing entries older than configurable threshold
- Bulk archive to `archive/`
- Mark-as-stale semantics

### Restructure / Reindex
- Restructure prompt + JSON action plan
- Apply actions: keep, rename, split, merge, move, delete
- Preview + confirm UI before applying

### LLM-assisted Lookup ("Ask AI")
- Send query + INDEX to LLM, fetch suggested files, return synthesized answer
- Token-budget awareness, snippet extraction

### MCP-side local clone & real git operations
- `git clone` of repo at `~/.claude/team-memory/<repo>/`
- MCP-handled commits with `git pull --rebase` then push
- PR fallback when rebase fails — open a PR via GitHub API and surface to user
- Continue trying to push to master after PR is opened
- Periodic `git pull` to keep cache fresh

### CLI
- `team-memory` shim binary
- `team-memory setup` (OAuth/PAT + MCP config)
- `team-memory remember --scope --type --text`
- `team-memory forget "<query>"`
- `team-memory restructure`

### Claude Code integration
- Session-end hook: 1-sentence + 3-bullet summary, opt-in save
- SessionStart hook for context loading from memory
- Slash command(s) for in-session memory access

### Distribution & polish
- Prebuilt MCP binaries: Windows, macOS (Intel/ARM), Linux (x86_64/ARM)
- Installer scripts / marketplace plugin
- Mobile styling pass
- Full integration test suite (browser + GitHub + MCP end-to-end)
- Security checklist + threat model doc
- Prompt-injection mitigations (entry sanitization, role separation)

### Multi-user / team
- Per-user namespacing beyond `users/<github-login>/`
- Conflict resolution UX for concurrent team edits
- Optional shared review workflow for `shared/` writes
