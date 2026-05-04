#!/usr/bin/env bash
# Reads the prompt from argv (after `-p`) and prints a fixed stream-json fixture.
# Used to test claude.go without depending on the real claude CLI.
cat "$(dirname "$0")/stream_categorize.jsonl"
