#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p mcp/prompts
cp prompts/*.txt mcp/prompts/
echo "prompts copied to mcp/prompts"

# Embed the frontend into the mcp binary
rm -rf mcp/frontend
cp -r frontend mcp/frontend
rm -rf mcp/frontend/prompts
echo "frontend copied to mcp/frontend for embedding"
