package hook

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTranscriptFile(t *testing.T) {
	jsonl := `{"type":"attachment"}
{"type":"user","message":{"role":"user","content":"Add retries to the GitHub client"}}
{"type":"user","isMeta":true,"message":{"role":"user","content":"<meta>"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"I'll add a retry loop."}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Edit","input":{}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}
{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"subagent chatter"}]}}
not json
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done."}]}}
`
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(jsonl), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTranscriptFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []Message{
		{Role: "user", Content: "Add retries to the GitHub client"},
		{Role: "assistant", Content: "I'll add a retry loop."},
		{Role: "assistant", Content: "Done."},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d messages %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("msg %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestReadTranscriptFile_Missing(t *testing.T) {
	if _, err := ReadTranscriptFile(filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Fatal("expected error for missing file")
	}
}
