#!/bin/sh
# team-memory installer for macOS and Linux.
#
#   curl -LsSf https://raw.githubusercontent.com/AndrewSkea/team-memory/master/install.sh | sh
#
# Extra arguments are passed to `team-memory-mcp install`, e.g.
#   curl -LsSf …/install.sh | sh -s -- --repo acme/team-memory --use-gh
#
# Environment: TEAM_MEMORY_VERSION (e.g. v1.0.0, default latest),
#              TEAM_MEMORY_BIN_DIR (default ~/bin),
#              TEAM_MEMORY_DOWNLOAD_BASE (mirror URL containing the release assets)
set -eu

REPO="AndrewSkea/team-memory"
BINARY="team-memory-mcp"
BIN_DIR="${TEAM_MEMORY_BIN_DIR:-$HOME/bin}"
VERSION="${TEAM_MEMORY_VERSION:-latest}"

err() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || err "'$1' is required"; }
need curl
need tar
need uname

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) err "unsupported architecture: $ARCH" ;;
esac
case "$OS" in
  linux|darwin) ;;
  *) err "unsupported OS: $OS — on Windows use install.ps1" ;;
esac

ARCHIVE="${BINARY}_${OS}_${ARCH}.tar.gz"
if [ -n "${TEAM_MEMORY_DOWNLOAD_BASE:-}" ]; then
  BASE="$TEAM_MEMORY_DOWNLOAD_BASE"   # mirrors / testing
elif [ "$VERSION" = "latest" ]; then
  BASE="https://github.com/$REPO/releases/latest/download"
else
  BASE="https://github.com/$REPO/releases/download/$VERSION"
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

echo "Downloading $BINARY ($VERSION, $OS/$ARCH)…"
curl -fsSL "$BASE/$ARCHIVE" -o "$TMP/$ARCHIVE" || err "download failed: $BASE/$ARCHIVE"
curl -fsSL "$BASE/checksums.txt" -o "$TMP/checksums.txt" || err "download failed: $BASE/checksums.txt"

# ── verify SHA-256 ───────────────────────────────────────────────────────────
EXPECTED=$(awk -v f="$ARCHIVE" '$2 == f { print $1 }' "$TMP/checksums.txt")
[ -n "$EXPECTED" ] || err "$ARCHIVE not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "$TMP/$ARCHIVE" | awk '{print $1}')
else
  ACTUAL=$(shasum -a 256 "$TMP/$ARCHIVE" | awk '{print $1}')
fi
[ "$EXPECTED" = "$ACTUAL" ] || err "checksum mismatch (expected $EXPECTED, got $ACTUAL)"

# ── install binary ───────────────────────────────────────────────────────────
mkdir -p "$BIN_DIR"
tar -xzf "$TMP/$ARCHIVE" -C "$TMP" "$BINARY"
# Replace atomically (new inode): on macOS, overwriting a signed binary in
# place makes the kernel kill later launches with "Code Signature Invalid".
cp "$TMP/$BINARY" "$BIN_DIR/.$BINARY.new"
chmod 755 "$BIN_DIR/.$BINARY.new"
mv -f "$BIN_DIR/.$BINARY.new" "$BIN_DIR/$BINARY"
echo "Installed $BIN_DIR/$BINARY ($("$BIN_DIR/$BINARY" version))"

# ── put BIN_DIR on PATH for future shells ────────────────────────────────────
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    case "$(basename "${SHELL:-sh}")" in
      zsh)  RC="$HOME/.zshrc" ;;
      bash) if [ "$OS" = "darwin" ]; then RC="$HOME/.bash_profile"; else RC="$HOME/.bashrc"; fi ;;
      fish) RC="" ; echo "Add $BIN_DIR to your PATH: fish_add_path $BIN_DIR" ;;
      *)    RC="$HOME/.profile" ;;
    esac
    if [ -n "$RC" ] && ! grep -qsF "$BIN_DIR" "$RC"; then
      printf '\n# added by team-memory installer\nexport PATH="%s:$PATH"\n' "$BIN_DIR" >> "$RC"
      echo "Added $BIN_DIR to PATH in $RC (open a new terminal to use it)"
    fi
    ;;
esac

# ── configure: memory repo, Claude Code hooks, MCP server ────────────────────
# The binary reads answers from /dev/tty itself, so this works under `curl | sh`.
# (exec skips the EXIT trap, so clean up first.)
rm -rf "$TMP"
trap - EXIT INT TERM
exec "$BIN_DIR/$BINARY" install "$@"
