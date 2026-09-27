#!/usr/bin/env bash
# Echoes the prompt received on stdin back as an assistant message, plus the
# nested-session marker, so tests can check how claude is invoked.
prompt=$(cat)
python3 - "$prompt" "${TEAM_MEMORY_NESTED:-}" "$*" <<'PY' 2>/dev/null || printf '{"type":"assistant","message":{"content":[{"type":"text","text":"%s|%s"}]}}\n' "$prompt" "${TEAM_MEMORY_NESTED:-}"
import json, sys
print(json.dumps({"type": "assistant", "message": {"content": [{"type": "text", "text": sys.argv[1] + "|" + sys.argv[2] + "|" + sys.argv[3]}]}}))
PY
