package llm

import (
	"os"
	"strings"
	"testing"
)

func TestExtractAssistantText(t *testing.T) {
	data, err := os.ReadFile("testdata/stream_categorize.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExtractAssistantText(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"target_file":"shared/programming-practices.md"`) {
		t.Errorf("missing target_file in extracted text:\n%s", got)
	}
	if !strings.Contains(got, `"unsure":false`) {
		t.Errorf("missing unsure field in extracted text:\n%s", got)
	}
}

func TestExtractAssistantTextErrorEvent(t *testing.T) {
	input := `{"type":"system","subtype":"init"}
{"type":"result","subtype":"error","is_error":true,"result":"boom"}
`
	_, err := ExtractAssistantText(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
