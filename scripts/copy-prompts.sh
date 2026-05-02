#!/usr/bin/env bash
set -euo pipefail
cp prompts/*.txt frontend/prompts/
cp prompts/*.txt mcp/prompts/
echo "prompts copied to frontend/prompts and mcp/prompts"
