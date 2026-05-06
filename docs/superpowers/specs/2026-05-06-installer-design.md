# Installer Design — team-memory

## Goal

Ship `install.sh` and `install.ps1` so users can install `team-memory-mcp` with a single command, no Go or package manager required. The scripts download a pre-built binary from GitHub Releases, write config interactively, wire Claude Code hooks, and register the MCP server.

## Release Pipeline

**Files:** `.goreleaser.yaml`, `.github/workflows/release.yml`

Trigger: push of a `v*` tag (e.g. `v0.1.0`).

GoReleaser builds five targets inside CI — no local GoReleaser install required:

| OS      | Arch          | Archive       |
|---------|---------------|---------------|
| linux   | amd64, arm64  | `.tar.gz`     |
| darwin  | amd64, arm64  | `.tar.gz`     |
| windows | amd64         | `.zip`        |

Each GitHub Release contains the archives plus `checksums.txt` (SHA256 of each archive).

The Go build runs inside `goreleaser/goreleaser-action`; the workflow sets up Go via `actions/setup-go` first.

## install.sh (Linux / macOS)

```
curl -LsSf https://raw.githubusercontent.com/AndrewSkea/team-memory/main/install.sh | sh
```

Steps:

1. Detect OS (`uname -s`) and arch (`uname -m`), map to GoReleaser archive name.
2. Fetch latest release tag from GitHub API (unauthenticated, public repo).
3. Download archive + `checksums.txt` to `mktemp` directory.
4. Verify SHA256 — `sha256sum` on Linux, `shasum -a 256` on macOS. Exit 1 on mismatch.
5. Extract `team-memory-mcp` binary to `~/bin/`.
6. Add `~/bin` to PATH — append one line to `~/.zshrc` (if zsh), `~/.bashrc` (if bash), `~/.profile` as fallback. Skip if already present.
7. Prompt for GitHub PAT (`read -s` — masked): fine-grained token with `contents:write` on the memory repo.
8. Prompt for repo in `owner/name` format.
9. Write `~/.config/team-memory/config.json`: `{"token":"…","owner":"…","repo":"…","check_first":false}`.
10. Wire `Stop` and `PreCompact` hooks into `~/.claude/settings.json` via `python3` inline script. Skip each hook if already present.
11. Register MCP server in `~/.claude.json` via `python3` inline script — adds `mcpServers.team-memory` with command `team-memory-mcp` and env vars `MEMORY_API_URL`/`MEMORY_API_KEY` if the key is needed, or minimal env block otherwise. Creates the file with `{}` base if missing.
12. Print success summary (see below).

JSON patching uses `python3 -c` with stdlib `json` only — no third-party deps.

## install.ps1 (Windows)

```powershell
irm https://raw.githubusercontent.com/AndrewSkea/team-memory/main/install.ps1 | iex
```

Same logical steps, Windows-adapted:

1. Detect arch via `$env:PROCESSOR_ARCHITECTURE`.
2. Fetch latest release tag from GitHub API (`Invoke-RestMethod`).
3. Download `.zip` + `checksums.txt` to `$env:TEMP`.
4. Verify SHA256 via `Get-FileHash` (built into PowerShell 4+). Exit on mismatch.
5. Extract `team-memory-mcp.exe` to `$env:USERPROFILE\bin\`.
6. Add `$env:USERPROFILE\bin` to User-scope PATH via `[Environment]::SetEnvironmentVariable`. No admin rights required. Skip if already present.
7. Prompt for GitHub PAT via `Read-Host -AsSecureString`, convert to plain string for writing.
8. Prompt for repo in `owner/name` format.
9. Write `$env:APPDATA\team-memory\config.json`.
10. Wire hooks into `$env:USERPROFILE\.claude\settings.json` via `python3 -c` inline script.
11. Register MCP server in `$env:USERPROFILE\.claude.json` via `python3 -c` inline script.
12. Print success summary.

Python3 is assumed present — Claude Code requires it, so any user of this tool will have it.

## Config File Schema

Written by both install scripts to the platform config dir:

```json
{
  "token": "<github-pat>",
  "owner": "<owner>",
  "repo": "<repo>",
  "check_first": false
}
```

Permissions: `0600` (bash) / owner-only ACL (PowerShell `Set-Content` with no explicit ACL — acceptable; tighten later if needed).

## MCP Registration

Patch applied to `~/.claude.json` (`%USERPROFILE%\.claude.json` on Windows):

```json
{
  "mcpServers": {
    "team-memory": {
      "command": "team-memory-mcp",
      "env": {
        "MEMORY_API_URL": "http://localhost:8000"
      }
    }
  }
}
```

If the file already has `mcpServers.team-memory`, overwrite it (idempotent re-install).

## Hook Registration

Patch applied to `~/.claude/settings.json`:

```json
{
  "hooks": {
    "Stop": [{ "matcher": "", "hooks": [{ "type": "command", "command": "team-memory-mcp --once session-end", "timeout": 60 }] }],
    "PreCompact": [{ "matcher": "", "hooks": [{ "type": "command", "command": "team-memory-mcp --once precompact", "timeout": 60 }] }]
  }
}
```

Each hook entry is skipped if a matching `team-memory-mcp` command already exists in that event's list.

## Post-install Output

```
✓ team-memory-mcp installed

  Binary:  ~/bin/team-memory-mcp
  Config:  ~/.config/team-memory/config.json
  Hooks:   ~/.claude/settings.json  (Stop, PreCompact)
  MCP:     ~/.claude.json           (team-memory server)

  Restart your terminal for PATH changes to take effect.
  Start a Claude Code session — it will auto-save to <owner>/<repo> when you stop.
```

## Error Handling

- Checksum mismatch → print error, delete temp files, exit 1.
- PAT or repo left blank → skip config write, print warning that hooks/MCP were installed but binary will error until config exists. User can re-run or write config manually.
- JSON patch fails (malformed existing file) → print error with file path, exit 1. Do not silently corrupt the file.
- `~/.claude.json` missing → create it with `{}` base before patching.

## File Changes

| File | Action |
|------|--------|
| `.goreleaser.yaml` | New |
| `.github/workflows/release.yml` | New |
| `install.sh` | New |
| `install.ps1` | New |
| `scripts/install-hooks.sh` | Delete (absorbed into install scripts) |
| `Makefile` | Keep `install` target for local dev; remove hook-wiring step since hooks script is gone |
| `README.md` | Replace install section with one-liner curl/irm commands |
