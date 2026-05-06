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

# ── summary ──────────────────────────────────────────────────────────────────
echo ""
echo "✓ team-memory-mcp installed"
echo ""
echo "  Binary:  $BIN_DIR/$BINARY"
echo "  Config:  ${XDG_CONFIG_HOME:-$HOME/.config}/team-memory/config.json"
echo "  Hooks:   $HOME/.claude/settings.json  (Stop, PreCompact)"
echo ""
echo "  Restart your terminal for PATH changes to take effect."
if [ -n "$SLUG" ]; then
  echo "  Start a Claude Code session — it will auto-save to $SLUG when you stop."
fi
