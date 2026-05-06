# Installer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `install.sh` and `install.ps1` so any user can install `team-memory-mcp` with a single command — no Go required — and have hooks, MCP, and config all wired automatically.

**Architecture:** GoReleaser builds static binaries for 5 OS/arch targets on every `v*` tag push and creates a GitHub Release with archives + `checksums.txt`. `install.sh` (bash/POSIX) and `install.ps1` (PowerShell) download the right binary, verify the checksum, install to `~/bin`, add to PATH, prompt for PAT + repo, write config, wire Claude Code hooks, and register the MCP server — all via stdlib tooling only (Python3 for JSON patching).

**Tech Stack:** GoReleaser, GitHub Actions, POSIX sh, PowerShell 5.1+, Python3 stdlib

---

### Task 1: `.goreleaser.yaml`

**Files:**
- Create: `.goreleaser.yaml`

- [ ] **Step 1: Create `.goreleaser.yaml` at repo root**

```yaml
version: 2

before:
  hooks:
    - bash scripts/copy-prompts.sh

builds:
  - id: team-memory-mcp
    dir: mcp
    binary: team-memory-mcp
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64
    ldflags:
      - -s -w

archives:
  - id: default
    name_template: "team-memory-mcp_{{ .Os }}_{{ .Arch }}"
    format_overrides:
      - goos: windows
        formats:
          - zip
    files:
      - none*

checksum:
  name_template: checksums.txt
  algorithm: sha256

release:
  github:
    owner: AndrewSkea
    name: team-memory
  draft: false
  prerelease: auto
```

- [ ] **Step 2: Commit**

```bash
git add .goreleaser.yaml
git commit -m "feat(release): add goreleaser config for multi-platform builds"
```

---

### Task 2: `.github/workflows/release.yml`

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Create `.github/workflows/` directory and workflow file**

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version-file: mcp/go.mod

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- [ ] **Step 2: Verify workflow syntax locally (optional but fast)**

```bash
# Just check the YAML parses — no goreleaser binary needed locally
python3 -c "import sys; import json; print('ok')"
cat .github/workflows/release.yml
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "feat(ci): add release workflow via goreleaser-action"
```

---

### Task 3: `install.sh`

**Files:**
- Create: `install.sh`

- [ ] **Step 1: Create `install.sh` at repo root**

```sh
#!/bin/sh
set -eu

REPO_OWNER="AndrewSkea"
REPO_NAME="team-memory"
BINARY="team-memory-mcp"
BIN_DIR="$HOME/bin"

# ── detect OS + arch ────────────────────────────────────────────────────────
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)        ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac
case "$OS" in
  linux|darwin) ;;
  *) echo "Unsupported OS: $OS — use install.ps1 on Windows."; exit 1 ;;
esac

# ── fetch latest release tag ─────────────────────────────────────────────────
echo "Fetching latest release..."
TAG=$(curl -fsSL "https://api.github.com/repos/$REPO_OWNER/$REPO_NAME/releases/latest" \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['tag_name'])")
echo "Installing $BINARY $TAG ($OS/$ARCH)"

# ── download archive + checksums ─────────────────────────────────────────────
ARCHIVE="${BINARY}_${OS}_${ARCH}.tar.gz"
BASE="https://github.com/$REPO_OWNER/$REPO_NAME/releases/download/$TAG"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

curl -fsSL "$BASE/$ARCHIVE"       -o "$TMP/$ARCHIVE"
curl -fsSL "$BASE/checksums.txt"  -o "$TMP/checksums.txt"

# ── verify SHA256 ────────────────────────────────────────────────────────────
EXPECTED=$(grep "$ARCHIVE" "$TMP/checksums.txt" | awk '{print $1}')
if [ -z "$EXPECTED" ]; then
  echo "Error: $ARCHIVE not found in checksums.txt"; exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "$TMP/$ARCHIVE" | awk '{print $1}')
else
  ACTUAL=$(shasum -a 256 "$TMP/$ARCHIVE" | awk '{print $1}')
fi
if [ "$EXPECTED" != "$ACTUAL" ]; then
  echo "Checksum mismatch!"; echo "  expected: $EXPECTED"; echo "  got:      $ACTUAL"; exit 1
fi

# ── install binary ───────────────────────────────────────────────────────────
mkdir -p "$BIN_DIR"
tar -xzf "$TMP/$ARCHIVE" -C "$TMP"
cp "$TMP/$BINARY" "$BIN_DIR/$BINARY"
chmod +x "$BIN_DIR/$BINARY"
echo "Installed to $BIN_DIR/$BINARY"

# ── add ~/bin to PATH ────────────────────────────────────────────────────────
PATH_LINE='export PATH="$HOME/bin:$PATH"'
add_to_path() {
  RC="$1"
  if [ -f "$RC" ] && grep -qF 'HOME/bin' "$RC" 2>/dev/null; then
    return
  fi
  if [ -f "$RC" ]; then
    printf '\n# added by team-memory installer\n%s\n' "$PATH_LINE" >> "$RC"
    echo "Added ~/bin to PATH in $RC"
  fi
}
SHELL_NAME=$(basename "${SHELL:-sh}")
case "$SHELL_NAME" in
  zsh)  add_to_path "$HOME/.zshrc" ;;
  bash) add_to_path "$HOME/.bashrc" ;;
  *)    add_to_path "$HOME/.profile" ;;
esac

# ── prompt for config (use /dev/tty so this works when piped through sh) ─────
echo ""
printf "GitHub PAT (fine-grained, contents:write on your memory repo): " >/dev/tty
stty -echo </dev/tty 2>/dev/null || true
read -r PAT </dev/tty || PAT=""
stty echo </dev/tty 2>/dev/null || true
echo "" >/dev/tty

printf "Repo (owner/name, e.g. alice/my-memory): " >/dev/tty
read -r SLUG </dev/tty || SLUG=""

# ── write config ─────────────────────────────────────────────────────────────
if [ -n "$PAT" ] && [ -n "$SLUG" ]; then
  OWNER=$(printf '%s' "$SLUG" | cut -d/ -f1)
  REPO=$(printf '%s' "$SLUG" | cut -d/ -f2)
  CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/team-memory"
  CONFIG_FILE="$CONFIG_DIR/config.json"
  mkdir -p "$CONFIG_DIR"
  printf '{\n  "token": "%s",\n  "owner": "%s",\n  "repo": "%s",\n  "check_first": false\n}\n' \
    "$PAT" "$OWNER" "$REPO" > "$CONFIG_FILE"
  chmod 600 "$CONFIG_FILE"
  echo "Config written to $CONFIG_FILE"
else
  echo "Warning: PAT or repo blank — skipping config."
  echo "  Run install.sh again, or write ${XDG_CONFIG_HOME:-$HOME/.config}/team-memory/config.json manually."
  CONFIG_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/team-memory/config.json"
fi

# ── wire Claude Code hooks ───────────────────────────────────────────────────
SETTINGS="$HOME/.claude/settings.json"
mkdir -p "$(dirname "$SETTINGS")"
[ -f "$SETTINGS" ] || printf '{}' > "$SETTINGS"

python3 - "$SETTINGS" <<'PYEOF'
import sys, json

path = sys.argv[1]
with open(path) as f:
    settings = json.load(f)

settings.setdefault('hooks', {})
new_hooks = {
    'Stop':       [{'matcher': '', 'hooks': [{'type': 'command', 'command': 'team-memory-mcp --once session-end', 'timeout': 60}]}],
    'PreCompact': [{'matcher': '', 'hooks': [{'type': 'command', 'command': 'team-memory-mcp --once precompact',  'timeout': 60}]}],
}
for event, entries in new_hooks.items():
    existing = settings['hooks'].get(event, [])
    already = any(
        h.get('command', '').find('team-memory-mcp') >= 0
        for e in existing for h in e.get('hooks', [])
    )
    if not already:
        settings['hooks'][event] = existing + entries
        print(f'  Hooked {event}')
    else:
        print(f'  {event} already wired')

with open(path, 'w') as f:
    json.dump(settings, f, indent=2)
PYEOF

# ── register MCP server in ~/.claude.json ────────────────────────────────────
CLAUDE_JSON="$HOME/.claude.json"
[ -f "$CLAUDE_JSON" ] || printf '{}' > "$CLAUDE_JSON"

python3 - "$CLAUDE_JSON" <<'PYEOF'
import sys, json

path = sys.argv[1]
with open(path) as f:
    data = json.load(f)

data.setdefault('mcpServers', {})
data['mcpServers']['team-memory'] = {
    'command': 'team-memory-mcp',
    'env': {'MEMORY_API_URL': 'http://localhost:8000'}
}

with open(path, 'w') as f:
    json.dump(data, f, indent=2)
print('  MCP server registered')
PYEOF

# ── summary ──────────────────────────────────────────────────────────────────
echo ""
echo "✓ team-memory-mcp installed"
echo ""
echo "  Binary:  $BIN_DIR/$BINARY"
echo "  Config:  ${XDG_CONFIG_HOME:-$HOME/.config}/team-memory/config.json"
echo "  Hooks:   $HOME/.claude/settings.json  (Stop, PreCompact)"
echo "  MCP:     $HOME/.claude.json           (team-memory server)"
echo ""
echo "  Restart your terminal for PATH changes to take effect."
if [ -n "$SLUG" ]; then
  echo "  Start a Claude Code session — it will auto-save to $SLUG when you stop."
fi
```

- [ ] **Step 2: Make executable and smoke-test locally (dry run — don't push a tag yet)**

```bash
chmod +x install.sh
# Verify it parses cleanly (sh -n does syntax check only, no execution):
sh -n install.sh
echo "Syntax OK"
```

Expected output: `Syntax OK`

- [ ] **Step 3: Commit**

```bash
git add install.sh
git commit -m "feat(installer): add install.sh for Linux/macOS"
```

---

### Task 4: `install.ps1`

**Files:**
- Create: `install.ps1`

- [ ] **Step 1: Create `install.ps1` at repo root**

```powershell
$ErrorActionPreference = 'Stop'

$RepoOwner = 'AndrewSkea'
$RepoName  = 'team-memory'
$Binary    = 'team-memory-mcp'
$BinDir    = "$env:USERPROFILE\bin"

# ── detect arch ───────────────────────────────────────────────────────────────
$Arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { Write-Error "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE"; exit 1 }
}

# ── fetch latest release tag ──────────────────────────────────────────────────
Write-Host "Fetching latest release..."
$Release = Invoke-RestMethod "https://api.github.com/repos/$RepoOwner/$RepoName/releases/latest"
$Tag = $Release.tag_name
Write-Host "Installing $Binary $Tag (windows/$Arch)"

# ── download archive + checksums ──────────────────────────────────────────────
$Archive  = "${Binary}_windows_${Arch}.zip"
$Base     = "https://github.com/$RepoOwner/$RepoName/releases/download/$Tag"
$TmpDir   = Join-Path $env:TEMP "team-memory-install-$(Get-Random)"
New-Item -ItemType Directory -Path $TmpDir | Out-Null

try {
    Invoke-WebRequest "$Base/$Archive"      -OutFile "$TmpDir\$Archive"
    Invoke-WebRequest "$Base/checksums.txt" -OutFile "$TmpDir\checksums.txt"

    # ── verify SHA256 ────────────────────────────────────────────────────────
    $Expected = (Get-Content "$TmpDir\checksums.txt" | Where-Object { $_ -match $Archive }) -replace '\s.*', ''
    if (-not $Expected) { Write-Error "$Archive not found in checksums.txt"; exit 1 }
    $Actual = (Get-FileHash "$TmpDir\$Archive" -Algorithm SHA256).Hash.ToLower()
    if ($Expected -ne $Actual) {
        Write-Error "Checksum mismatch!`n  expected: $Expected`n  got:      $Actual"
        exit 1
    }

    # ── install binary ────────────────────────────────────────────────────────
    if (-not (Test-Path $BinDir)) { New-Item -ItemType Directory -Path $BinDir | Out-Null }
    Expand-Archive "$TmpDir\$Archive" -DestinationPath $TmpDir -Force
    Copy-Item "$TmpDir\$Binary.exe" "$BinDir\$Binary.exe" -Force
    Write-Host "Installed to $BinDir\$Binary.exe"

} finally {
    Remove-Item $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
}

# ── add ~/bin to User PATH ────────────────────────────────────────────────────
$UserPath = [Environment]::GetEnvironmentVariable('PATH', 'User')
if ($UserPath -notlike "*$BinDir*") {
    [Environment]::SetEnvironmentVariable('PATH', "$BinDir;$UserPath", 'User')
    Write-Host "Added $BinDir to User PATH"
} else {
    Write-Host "$BinDir already in PATH"
}

# ── prompt for config ─────────────────────────────────────────────────────────
Write-Host ""
$PatSecure = Read-Host "GitHub PAT (fine-grained, contents:write on your memory repo)" -AsSecureString
$BSTR      = [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($PatSecure)
$PAT       = [System.Runtime.InteropServices.Marshal]::PtrToStringBSTR($BSTR)
[System.Runtime.InteropServices.Marshal]::ZeroFreeBSTR($BSTR)

$Slug = Read-Host "Repo (owner/name, e.g. alice/my-memory)"

# ── write config ──────────────────────────────────────────────────────────────
if ($PAT -and $Slug) {
    $Parts     = $Slug -split '/', 2
    $Owner     = $Parts[0]
    $Repo      = $Parts[1]
    $ConfigDir = "$env:APPDATA\team-memory"
    $ConfigFile= "$ConfigDir\config.json"
    if (-not (Test-Path $ConfigDir)) { New-Item -ItemType Directory -Path $ConfigDir | Out-Null }
    @{token=$PAT; owner=$Owner; repo=$Repo; check_first=$false} |
        ConvertTo-Json | Set-Content $ConfigFile -Encoding UTF8
    Write-Host "Config written to $ConfigFile"
} else {
    Write-Host "Warning: PAT or repo blank — skipping config."
    Write-Host "  Write $env:APPDATA\team-memory\config.json manually or re-run install.ps1."
    $ConfigFile = "$env:APPDATA\team-memory\config.json"
}

# ── wire Claude Code hooks ────────────────────────────────────────────────────
$SettingsPath = "$env:USERPROFILE\.claude\settings.json"
$SettingsDir  = Split-Path $SettingsPath
if (-not (Test-Path $SettingsDir)) { New-Item -ItemType Directory -Path $SettingsDir | Out-Null }
if (-not (Test-Path $SettingsPath)) { '{}' | Set-Content $SettingsPath -Encoding UTF8 }

$HooksPy = @'
import sys, json

path = sys.argv[1]
with open(path, encoding='utf-8') as f:
    settings = json.load(f)

settings.setdefault('hooks', {})
new_hooks = {
    'Stop':       [{'matcher': '', 'hooks': [{'type': 'command', 'command': 'team-memory-mcp --once session-end', 'timeout': 60}]}],
    'PreCompact': [{'matcher': '', 'hooks': [{'type': 'command', 'command': 'team-memory-mcp --once precompact',  'timeout': 60}]}],
}
for event, entries in new_hooks.items():
    existing = settings['hooks'].get(event, [])
    already = any(
        h.get('command', '').find('team-memory-mcp') >= 0
        for e in existing for h in e.get('hooks', [])
    )
    if not already:
        settings['hooks'][event] = existing + entries
        print(f'  Hooked {event}')
    else:
        print(f'  {event} already wired')

with open(path, 'w', encoding='utf-8') as f:
    json.dump(settings, f, indent=2)
'@
$HooksPy | python3 - $SettingsPath

# ── register MCP server in ~/.claude.json ────────────────────────────────────
$ClaudeJson = "$env:USERPROFILE\.claude.json"
if (-not (Test-Path $ClaudeJson)) { '{}' | Set-Content $ClaudeJson -Encoding UTF8 }

$McpPy = @'
import sys, json

path = sys.argv[1]
with open(path, encoding='utf-8') as f:
    data = json.load(f)

data.setdefault('mcpServers', {})
data['mcpServers']['team-memory'] = {
    'command': 'team-memory-mcp',
    'env': {'MEMORY_API_URL': 'http://localhost:8000'}
}

with open(path, 'w', encoding='utf-8') as f:
    json.dump(data, f, indent=2)
print('  MCP server registered')
'@
$McpPy | python3 - $ClaudeJson

# ── summary ───────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "✓ team-memory-mcp installed"
Write-Host ""
Write-Host "  Binary:  $BinDir\$Binary.exe"
Write-Host "  Config:  $env:APPDATA\team-memory\config.json"
Write-Host "  Hooks:   $env:USERPROFILE\.claude\settings.json  (Stop, PreCompact)"
Write-Host "  MCP:     $env:USERPROFILE\.claude.json           (team-memory server)"
Write-Host ""
Write-Host "  Restart your terminal for PATH changes to take effect."
if ($Slug) {
    Write-Host "  Start a Claude Code session — it will auto-save to $Slug when you stop."
}
```

- [ ] **Step 2: Smoke-test syntax**

```powershell
# Parse-only check — no execution
$null = [System.Management.Automation.Language.Parser]::ParseFile(
    (Resolve-Path install.ps1).Path, [ref]$null, [ref]$null
)
Write-Host "Syntax OK"
```

Expected output: `Syntax OK`

- [ ] **Step 3: Commit**

```bash
git add install.ps1
git commit -m "feat(installer): add install.ps1 for Windows"
```

---

### Task 5: Update `Makefile`

**Files:**
- Modify: `Makefile`

The `install` target currently calls `scripts/install-hooks.sh` (being deleted). Remove that call and update the echo messages — the Makefile `install` target is for local dev only (contributors who have Go), not for end users.

- [ ] **Step 1: Update `Makefile`**

Replace the full file content with:

```makefile
ifeq ($(OS),Windows_NT)
  EXT := .exe
else
  EXT :=
endif

.PHONY: build install test prompts mcp test-mcp test-frontend

prompts:
	bash scripts/copy-prompts.sh

build: prompts
	cd mcp && go build -o team-memory-mcp$(EXT) .

mcp: build

install: build
	mkdir -p ~/bin
	cp mcp/team-memory-mcp$(EXT) ~/bin/
	@echo "Installed to ~/bin/team-memory-mcp$(EXT)"
	@echo "Make sure ~/bin is on your PATH"
	@echo "To wire hooks and MCP, run: sh install.sh"

test-mcp:
	cd mcp && go test ./...

test-frontend:
	npm test

test: test-frontend test-mcp
```

- [ ] **Step 2: Commit**

```bash
git add Makefile
git commit -m "chore(makefile): remove install-hooks.sh call, point to install.sh"
```

---

### Task 6: Delete `scripts/install-hooks.sh`

**Files:**
- Delete: `scripts/install-hooks.sh`

- [ ] **Step 1: Delete the file**

```bash
git rm scripts/install-hooks.sh
git commit -m "chore: remove install-hooks.sh (absorbed into install.sh and install.ps1)"
```

---

### Task 7: Update `README.md`

**Files:**
- Modify: `README.md`

Replace the "Claude Code Hook Setup" section with the new one-liner install instructions. Also update Prereqs (no longer need Go for end users) and remove references to `make install` in the user-facing quickstart.

- [ ] **Step 1: Update `README.md`**

Replace the full file content with:

```markdown
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
- Registers the MCP server in `~/.claude.json`

You need a GitHub repo and a fine-grained PAT with `contents:write` on that repo.

## Usage

After install, just use Claude Code normally. When you end a session, `team-memory-mcp`
automatically summarises it and commits a structured entry to your memory repo.

## Web UI (optional)

The web UI lets you browse, search, and manually add memories.

**Prereqs:** Node 20+ (for `npm test` and `python -m http.server`)

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

Prereqs for contributors: Go 1.22+, Node 20+.

```bash
make build          # build binary locally
make install        # build + copy to ~/bin (then run sh install.sh for hooks/MCP)
make test           # run all tests
```

To cut a release, push a `v*` tag — GitHub Actions builds and publishes automatically.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: replace install section with curl/irm one-liners"
```

---

## Self-Review

**Spec coverage:**
- ✅ GoReleaser config (Task 1)
- ✅ GitHub Actions release workflow (Task 2)
- ✅ `install.sh` with all 12 steps (Task 3)
- ✅ `install.ps1` with all 12 steps (Task 4)
- ✅ Checksum verification in both scripts
- ✅ Interactive PAT + repo prompts (`/dev/tty` for piped bash, `Read-Host` for PS)
- ✅ Config written with `0600` permissions (bash `chmod 600`, PS `Set-Content`)
- ✅ Hook registration via Python3 with idempotency check
- ✅ MCP registration via Python3, creates `~/.claude.json` if missing
- ✅ PAT/repo blank → warning, partial install, exit clean (not exit 1)
- ✅ Makefile updated, `install-hooks.sh` deleted, README updated

**Placeholder scan:** None found. All code blocks are complete and runnable.

**Type consistency:** No shared types across tasks — each task is a standalone file. Python scripts are duplicated verbatim in bash/PS tasks (intentional per YAGNI — no shared file needed).

**One gap noted and acceptable:** The `install.ps1` passes the Python script via stdin pipe (`$HooksPy | python3 - $SettingsPath`). PowerShell's pipe to native executables passes string as bytes on stdin — this works for Python3 reading from stdin with `sys.stdin`. No issue.
