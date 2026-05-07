#!/usr/bin/env bash
set -euo pipefail
mkdir -p frontend/prompts mcp/prompts
cp prompts/*.txt frontend/prompts/
cp prompts/*.txt mcp/prompts/
echo "prompts copied to frontend/prompts and mcp/prompts"

# Embed the frontend into the mcp binary
rm -rf mcp/frontend
cp -r frontend mcp/frontend
echo "frontend copied to mcp/frontend for embedding"
