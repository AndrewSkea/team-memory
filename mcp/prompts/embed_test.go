package prompts

import (
	"strings"
	"testing"
)

func TestPromptsEmbedded(t *testing.T) {
	if len(Extract) < 500 || !strings.Contains(Extract, `"memories"`) {
		t.Errorf("Extract prompt missing or truncated: %d bytes", len(Extract))
	}
	if len(Classify) < 200 || !strings.Contains(Classify, `"title"`) {
		t.Errorf("Classify prompt missing or truncated: %d bytes", len(Classify))
	}
}
