// Package prompts embeds the LLM prompts. Source of truth is /prompts; run
// `make build` (scripts/copy-prompts.sh) to refresh the copies here.
package prompts

import _ "embed"

//go:embed extract.txt
var Extract string

//go:embed classify.txt
var Classify string
