package prompts

import "testing"

func TestPromptsEmbedded(t *testing.T) {
	if len(Categorize) < 100 {
		t.Errorf("Categorize prompt too short: %d bytes", len(Categorize))
	}
	if len(Summarize) < 50 {
		t.Errorf("Summarize prompt too short: %d bytes", len(Summarize))
	}
}
