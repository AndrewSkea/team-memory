# team-memory

Local-first memory app. All data lives in your own GitHub repo as Markdown.
`team-memory-mcp` auto-saves a structured summary of every Claude Code session
to your GitHub repo when the session ends.

## Install

**macOS / Linux:**
```sh
curl -LsSf https://raw.githubusercontent.com/AndrewSkea/team-memory/master/install.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/AndrewSkea/team-memory/master/install.ps1 | iex
```

The installer downloads and verifies the binary, adds it to `~/bin`, prompts for your
GitHub PAT and memory repo, wires `Stop`/`PreCompact` hooks into `~/.claude/settings.json`,
and registers the MCP server in `~/.claude.json`.

You need a GitHub repo and a fine-grained PAT with `contents:write` on that repo.

### Service mode (always-on web UI)

The installer offers two run modes. Service mode keeps the web UI available at all times:

| | Non-admin (default) | Admin (`Run as Administrator`) |
|---|---|---|
| Autostart | Registry `HKCU\...\Run` key | Task Scheduler (`RunLevel Highest`) |
| Binary port | 7438 | 7438 |
| Browser URL | `http://127.0.0.1:7438/` | `http://team-mem/` |
| `team-mem` hostname | Manual (see below) | Auto-added to hosts file |
| Port 80 redirect | - | `netsh interface portproxy` 80 -> 7438 |

The admin path sets up a `netsh interface portproxy` rule that forwards
`127.0.0.1:80` to `127.0.0.1:7438`, so `http://team-mem/` resolves via
the hosts file and hits the portproxy without requiring Go to bind port 80.

To get the `http://team-mem/` shortcut without re-running as admin, add one line to
`C:\Windows\System32\drivers\etc\hosts` (requires admin once):
```
127.0.0.1 team-mem
```
Then access via `http://team-mem:7438/` (no portproxy without admin).

## Usage

After install, just use Claude Code normally. When you end a session, `team-memory-mcp`
automatically summarises it and commits a structured entry to your memory repo.

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
