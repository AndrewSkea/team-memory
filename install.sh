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

# ── read existing config (if any) ────────────────────────────────────────────
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/team-memory"
CONFIG_FILE="$CONFIG_DIR/config.json"

EXISTING_PAT=""
EXISTING_SLUG=""
if [ -f "$CONFIG_FILE" ]; then
  EXISTING_PAT=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1])).get('token',''))" "$CONFIG_FILE" 2>/dev/null || true)
  EXISTING_OWNER=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1])).get('owner',''))" "$CONFIG_FILE" 2>/dev/null || true)
  EXISTING_REPO=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1])).get('repo',''))" "$CONFIG_FILE" 2>/dev/null || true)
  if [ -n "$EXISTING_OWNER" ] && [ -n "$EXISTING_REPO" ]; then
    EXISTING_SLUG="$EXISTING_OWNER/$EXISTING_REPO"
  fi
fi

# ── prompt for config ─────────────────────────────────────────────────────────
echo ""
if [ -n "$EXISTING_PAT" ]; then
  printf "GitHub PAT [keep existing]: " >/dev/tty
else
  printf "GitHub PAT (fine-grained, contents:write on your memory repo): " >/dev/tty
fi
stty -echo </dev/tty 2>/dev/null || true
read -r PAT </dev/tty || PAT=""
stty echo </dev/tty 2>/dev/null || true
echo "" >/dev/tty
[ -z "$PAT" ] && PAT="$EXISTING_PAT"

if [ -n "$EXISTING_SLUG" ]; then
  printf "Repo (owner/name) [${EXISTING_SLUG}]: " >/dev/tty
else
  printf "Repo (owner/name, e.g. alice/my-memory): " >/dev/tty
fi
read -r SLUG </dev/tty || SLUG=""
[ -z "$SLUG" ] && SLUG="$EXISTING_SLUG"

# ── write config ─────────────────────────────────────────────────────────────
if [ -n "$PAT" ] && [ -n "$SLUG" ]; then
  OWNER=$(printf '%s' "$SLUG" | cut -d/ -f1)
  REPO=$(printf '%s' "$SLUG" | cut -d/ -f2)
  mkdir -p "$CONFIG_DIR"
  # JSON-encode each field so quotes/backslashes in PAT/owner/repo can't
  # corrupt the file. (GitHub PATs don't contain these today but inputs
  # come from a TTY — defence in depth.)
  PAT="$PAT" OWNER="$OWNER" REPO="$REPO" python3 - "$CONFIG_FILE" <<'PYEOF'
import json, os, sys
out = {
    "token": os.environ["PAT"],
    "owner": os.environ["OWNER"],
    "repo":  os.environ["REPO"],
    "check_first": False,
}
with open(sys.argv[1], "w") as f:
    json.dump(out, f, indent=2)
PYEOF
  chmod 600 "$CONFIG_FILE"
  echo "Config written to $CONFIG_FILE"
else
  echo "Warning: PAT or repo blank — skipping config."
  echo "  Run install.sh again, or write $CONFIG_FILE manually."
fi

# ── choose run mode ───────────────────────────────────────────────────────────
echo ""
echo "How should team-memory-mcp run?"
echo "  1) System service  — always available; open http://team-mem/ in browser"
echo "  2) With Claude Code — auto-starts when Claude Code runs (lighter weight)"
printf "Choice [1/2, default 2]: " >/dev/tty
read -r RUN_CHOICE </dev/tty || RUN_CHOICE=""
[ -z "$RUN_CHOICE" ] && RUN_CHOICE="2"

# ── wire Claude Code hooks (both modes need hooks for session-end/precompact) ─
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

# In service mode the server is always running; disable auto-start from Claude Code.
# In claude-code mode, Claude Code auto-starts the process via the 'command' field.
if [ "$RUN_CHOICE" = "1" ]; then
  MCP_COMMAND="team-memory-mcp"
  MCP_ARGS='["--mcp", "--port", "7439"]'   # use 7439 so it doesn't conflict with service on 7438
  SERVICE_MODE=true
else
  MCP_COMMAND="team-memory-mcp"
  MCP_ARGS='["--mcp"]'
  SERVICE_MODE=false
fi

python3 - "$CLAUDE_JSON" "$MCP_COMMAND" "$MCP_ARGS" <<'PYEOF'
import sys, json

path, command, args_str = sys.argv[1], sys.argv[2], sys.argv[3]
args = json.loads(args_str)
with open(path) as f:
    data = json.load(f)

data.setdefault('mcpServers', {})
if 'team-memory' not in data['mcpServers']:
    data['mcpServers']['team-memory'] = {'command': command, 'args': args}
    print('  MCP server registered')
else:
    # Update args in case port changed
    data['mcpServers']['team-memory']['args'] = args
    print('  MCP server updated')

with open(path, 'w') as f:
    json.dump(data, f, indent=2)
PYEOF

# ── install Claude Code slash commands ───────────────────────────────────────
COMMANDS_DIR="$HOME/.claude/commands"
mkdir -p "$COMMANDS_DIR"

cat > "$COMMANDS_DIR/memory-search.md" <<'CMDEOF'
Search team memory for: $ARGUMENTS

Use the `lookup` MCP tool with "$ARGUMENTS" as the query. Present the results clearly, grouped by source file. If no results are found, say so briefly.
CMDEOF

cat > "$COMMANDS_DIR/memory-add.md" <<'CMDEOF'
Save the following note to team memory: $ARGUMENTS

Use the Bash tool to run this command — Python handles JSON encoding so quotes and special characters work correctly:

```bash
python3 - "$ARGUMENTS" << 'PYEOF'
import json, sys, urllib.request
note = sys.argv[1]
body = json.dumps({"text": note, "title": ""}).encode("utf-8")
req = urllib.request.Request(
    "http://127.0.0.1:7438/v1/quick-add",
    body,
    {"Content-Type": "application/json"}
)
with urllib.request.urlopen(req) as resp:
    print(resp.read().decode("utf-8"))
PYEOF
```

Parse the response JSON and report the `file` field (e.g. "Saved to DECISIONS.md"). If connection is refused, the server is not running — start it with `team-memory-mcp`.
CMDEOF

echo "  Commands: $COMMANDS_DIR  (memory-search, memory-add)"

# ── service setup ─────────────────────────────────────────────────────────────
PORT=7438

if [ "$RUN_CHOICE" = "1" ]; then
  # ── add hosts entry ──────────────────────────────────────────────────────
  if ! grep -qF 'team-mem' /etc/hosts 2>/dev/null; then
    if echo "127.0.0.1 team-mem" | sudo tee -a /etc/hosts >/dev/null 2>&1; then
      echo "  Added team-mem to /etc/hosts"
    else
      echo "  Could not write /etc/hosts (no sudo) — add manually:"
      echo "    echo '127.0.0.1 team-mem' | sudo tee -a /etc/hosts"
    fi
  else
    echo "  team-mem already in /etc/hosts"
  fi

  # ── try to grant port 80 binding (Linux only) ─────────────────────────────
  if [ "$OS" = "linux" ] && command -v setcap >/dev/null 2>&1; then
    if sudo setcap 'cap_net_bind_service=+ep' "$BIN_DIR/$BINARY" 2>/dev/null; then
      PORT=80
      echo "  Port 80 granted via setcap — browser URL: http://team-mem/"
    else
      echo "  setcap failed — browser URL: http://team-mem:7438/"
    fi
  else
    echo "  Browser URL: http://team-mem:7438/"
  fi

  if [ "$OS" = "linux" ]; then
    # ── systemd user service ───────────────────────────────────────────────
    SVCDIR="$HOME/.config/systemd/user"
    mkdir -p "$SVCDIR"
    cat > "$SVCDIR/team-memory-mcp.service" <<EOF
[Unit]
Description=Team Memory MCP Server
After=default.target

[Service]
ExecStart=$BIN_DIR/$BINARY --port $PORT
Restart=on-failure
RestartSec=5s
Environment=HOME=$HOME

[Install]
WantedBy=default.target
EOF
    systemctl --user daemon-reload
    systemctl --user enable --now team-memory-mcp
    echo "  systemd user service started"
    echo "  Manage: systemctl --user status team-memory-mcp"

  elif [ "$OS" = "darwin" ]; then
    # ── launchd user agent (macOS) ────────────────────────────────────────
    PLIST_DIR="$HOME/Library/LaunchAgents"
    PLIST="$PLIST_DIR/com.team-memory.mcp.plist"
    LOG_DIR="$HOME/.local/share/team-memory"
    mkdir -p "$PLIST_DIR" "$LOG_DIR"
    cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.team-memory.mcp</string>
  <key>ProgramArguments</key>
  <array>
    <string>$BIN_DIR/$BINARY</string>
    <string>--port</string>
    <string>$PORT</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>$LOG_DIR/mcp.log</string>
  <key>StandardErrorPath</key>
  <string>$LOG_DIR/mcp.log</string>
</dict>
</plist>
EOF
    # Unload existing if present, then load
    launchctl unload "$PLIST" 2>/dev/null || true
    launchctl load -w "$PLIST"
    echo "  launchd agent loaded"
    echo "  Manage: launchctl list | grep team-memory"
    echo "  Log:    tail -f $LOG_DIR/mcp.log"
    echo "  Browser URL: http://team-mem:$PORT/"
  fi
fi

# ── summary ──────────────────────────────────────────────────────────────────
echo ""
echo "✓ team-memory-mcp installed"
echo ""
echo "  Binary:  $BIN_DIR/$BINARY"
echo "  Config:  ${XDG_CONFIG_HOME:-$HOME/.config}/team-memory/config.json"
echo "  Hooks:   $HOME/.claude/settings.json  (Stop, PreCompact)"
echo "  MCP:     $HOME/.claude.json           (team-memory server)"
echo ""
if [ "$RUN_CHOICE" = "1" ]; then
  if [ "$PORT" = "80" ]; then
    echo "  Web UI:  http://team-mem/"
  else
    echo "  Web UI:  http://team-mem:$PORT/"
  fi
else
  echo "  Web UI:  http://127.0.0.1:7438/  (available while Claude Code is running)"
fi
echo ""
echo "  Restart your terminal for PATH changes to take effect."
