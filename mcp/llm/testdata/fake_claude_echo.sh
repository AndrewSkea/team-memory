#!/usr/bin/env bash
# Echoes the prompt received on stdin back as an assistant message, plus the
# nested-session marker and argv, so tests can check how claude is invoked.
# The prompt is piped (not passed as an argument) to stay under ARG_MAX.
NESTED="${TEAM_MEMORY_NESTED:-}" ARGS="$*" python3 -c '
import json, os, sys
prompt = sys.stdin.read()
text = prompt + "|" + os.environ["NESTED"] + "|" + os.environ["ARGS"]
print(json.dumps({"type": "assistant", "message": {"content": [{"type": "text", "text": text}]}}))
'
