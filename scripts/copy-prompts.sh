#!/usr/bin/env bash
set -euo pipefail
mkdir -p frontend/prompts mcp/prompts
cp prompts/*.txt frontend/prompts/
cp prompts/*.txt mcp/prompts/
echo "prompts copied to frontend/prompts and mcp/prompts"
