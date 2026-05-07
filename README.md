# team-memory

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Local-first memory for Claude Code. Every session is automatically summarised and committed to your own GitHub repo as Markdown. Browse, search, and recall knowledge through a clean web UI — no cloud, no subscriptions, your data stays yours.

## Features

- **Auto-save sessions** — a hook fires when Claude Code ends a session, summarises it with Claude, and commits a structured entry to your GitHub repo
- **Web UI** — browse entries, save new ones, run keyword lookups, and flag stale knowledge at `http://127.0.0.1:7438`
- **Smart categorisation** — LLM picks the right file from your index; or save without classification if you prefer
- **GitHub-backed storage** — all entries are plain Markdown in a repo you own; edit, search, and version-control them directly
- **MCP server** — exposes memory tools to Claude Code so the AI can read and write your knowledge base mid-session
- **No telemetry** — the binary binds `127.0.0.1` only and never phones home

## Install

**macOS / Linux:**
```sh
curl -LsSf https://raw.githubusercontent.com/AndrewSkea/team-memory/master/install.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/AndrewSkea/team-memory/master/install.ps1 | iex
```

The installer downloads the binary, adds it to `~/bin`, prompts for your GitHub PAT and memory repo, wires `Stop`/`PreCompact` hooks into `~/.claude/settings.json`, and registers the MCP server in `~/.claude.json`.

**Prerequisites:** a GitHub repo and a fine-grained PAT with `contents:write` on that repo.

### Service mode (always-on web UI)

The installer offers two run modes. Service mode keeps the web UI available at all times:

| | Non-admin (default) | Admin (`Run as Administrator`) |
|---|---|---|
| Autostart | Registry `HKCU\...\Run` key | Task Scheduler (`RunLevel Highest`) |
| Browser URL | `http://127.0.0.1:7438/` | `http://team-mem/` |
| `team-mem` hostname | Manual (see below) | Auto-added to hosts file |
| Port 80 redirect | — | `netsh interface portproxy` 80 → 7438 |

To get the `http://team-mem/` shortcut without re-running as admin, add one line to `C:\Windows\System32\drivers\etc\hosts` (requires admin once):
```
127.0.0.1 team-mem
```
Then access via `http://team-mem:7438/` (no portproxy without admin).

## Usage

After install, use Claude Code normally. When a session ends, `team-memory-mcp` automatically summarises it and commits a structured entry to your memory repo. Open `http://127.0.0.1:7438` to browse your knowledge base, save entries manually, or search by keyword.

## Privacy & security

- PAT and Anthropic key are stored in `~/.config/team-memory/config.json` (CLI) and your browser's `localStorage` (web UI)
- The binary binds `127.0.0.1` only — never exposed to the network
- No telemetry, no third-party scripts
- See [SECURITY.md](SECURITY.md) for the full threat model

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for build instructions, project layout, and code style.

```bash
make build   # build binary locally
make test    # run all tests
```

## License

[MIT](LICENSE) © Andrew Skea
