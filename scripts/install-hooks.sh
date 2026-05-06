#!/usr/bin/env bash
set -e

SETTINGS="$HOME/.claude/settings.json"

if [ ! -f "$SETTINGS" ]; then
  mkdir -p "$(dirname "$SETTINGS")"
  echo '{}' > "$SETTINGS"
fi

node - "$SETTINGS" <<'EOF'
const fs = require('fs');
const path = process.argv[2];
const settings = JSON.parse(fs.readFileSync(path, 'utf8'));

const newHooks = {
  Stop: [{
    matcher: "",
    hooks: [{ type: "command", command: "team-memory-mcp --once session-end", timeout: 60 }]
  }],
  PreCompact: [{
    matcher: "",
    hooks: [{ type: "command", command: "team-memory-mcp --once precompact", timeout: 60 }]
  }]
};

settings.hooks = settings.hooks || {};

for (const [event, entries] of Object.entries(newHooks)) {
  const existing = settings.hooks[event] || [];
  const alreadyWired = existing.some(e =>
    e.hooks && e.hooks.some(h => h.command && h.command.includes('team-memory-mcp'))
  );
  if (!alreadyWired) {
    settings.hooks[event] = [...existing, ...entries];
    console.log(`Added ${event} hook`);
  } else {
    console.log(`${event} hook already present, skipping`);
  }
}

fs.writeFileSync(path, JSON.stringify(settings, null, 2));
EOF

echo "Hooks installed in $SETTINGS"
