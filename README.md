# team-memory

Local-first memory app. All data lives in your own GitHub repo as Markdown.
`team-memory-mcp` auto-saves a structured summary of every Claude Code session
to your GitHub repo when the session ends.

## Install

**macOS / Linux:**
```sh
curl -LsSf https://raw.githubusercontent.com/AndrewSkea/team-memory/main/install.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/AndrewSkea/team-memory/main/install.ps1 | iex
```

The installer:
- Downloads the pre-built binary and verifies its SHA256 checksum
- Installs `team-memory-mcp` to `~/bin` and adds it to your PATH
- Prompts for your GitHub PAT and memory repo, writes `~/.config/team-memory/config.json`
- Wires `Stop` and `PreCompact` hooks into `~/.claude/settings.json`
- Registers the server in `~/.claude.json` so Claude Code auto-starts it

You need a GitHub repo and a fine-grained PAT with `contents:write` on that repo.

## Usage

After install, just use Claude Code normally. When you end a session, `team-memory-mcp`
automatically summarises it and commits a structured entry to your memory repo.

## Web UI (optional)

The web UI lets you browse, search, and manually add memories.

**Prereqs:** Node 20+

```bash
make prompts
npm run serve
# open http://localhost:8080
```

Setup page: paste PAT, owner/repo, and optionally an Anthropic API key.

## Tests

```bash
make test           # runs frontend + MCP tests
```

## Smoke checklist

1. Setup page accepts PAT + repo, shows "Authenticated as \<login\>".
2. If repo was empty, INDEX.md / GENERAL.md / UNSURE.md appear in GitHub.
3. Remember page → type "test memory" → Save → entry appears in GENERAL.md
   on GitHub with the right `### Entry:` block.
4. Auto-categorize button returns a target file and summary; saving commits to
   that file and updates INDEX.md.
5. Lookup page finds the new entry by keyword.
6. Pulling the repo from GitHub directly while save is in flight should
   produce a "file changed in GitHub" error after 3 retries (manual test).

## Privacy

- PAT and Anthropic key are stored in your browser's `localStorage` (web UI) and
  `~/.config/team-memory/config.json` (CLI). No telemetry, no third-party scripts.
- The MCP binds `127.0.0.1` only.

## Contributing

Prereqs: Go 1.22+, Node 20+.

```bash
make build          # build binary locally
make install        # build + copy to ~/bin (then run sh install.sh for hooks)
make test           # run all tests
```

To cut a release, push a `v*` tag — GitHub Actions builds and publishes automatically.
