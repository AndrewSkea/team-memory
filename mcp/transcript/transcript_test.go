package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `{"type":"attachment"}
{"type":"user","message":{"role":"user","content":"Add retries to the GitHub client"}}
{"type":"user","isMeta":true,"message":{"role":"user","content":"<meta>"}}
{"type":"user","message":{"role":"user","content":"<command-name>/clear</command-name>"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"I'll add a retry loop."}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Edit","input":{}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}
{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"subagent chatter"}]}}
not json
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done."}]}}
`

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadFile(t *testing.T) {
	got, err := ReadFile(writeFile(t, sample))
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

func TestReadFromResumesAndSkipsPartialLine(t *testing.T) {
	partial := sample + `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"half`
	path := writeFile(t, partial)
	msgs, n, err := ReadFrom(path, 0)
	if err != nil || len(msgs) != 3 || n != 10 {
		t.Fatalf("first read: %d msgs, n=%d, %v", len(msgs), n, err)
	}
	// Append more; resuming only returns the new messages.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(` written"}]}}` + "\n" + `{"type":"user","message":{"role":"user","content":"thanks"}}` + "\n")
	f.Close()
	msgs, n2, err := ReadFrom(path, n)
	if err != nil || len(msgs) != 2 || msgs[0].Content != "half written" || n2 != 12 {
		t.Fatalf("resume: %+v n=%d %v", msgs, n2, err)
	}
}

func TestMissing(t *testing.T) {
	if _, err := ReadFile(filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestRenderBudget(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "TASK: fix the payments retry bug"}}
	for i := 0; i < 200; i++ {
		msgs = append(msgs, Message{Role: "assistant", Content: strings.Repeat("x", 100)})
	}
	msgs = append(msgs, Message{Role: "assistant", Content: "FINAL: root cause was missing jitter"})
	out := Render(msgs, 2000)
	if len(out) > 2100 || !strings.Contains(out, "TASK:") || !strings.Contains(out, "FINAL:") || !strings.Contains(out, "omitted") {
		t.Fatalf("len %d:\n%s", len(out), out)
	}
	small := Render(msgs[:2], 10000)
	if strings.Contains(small, "omitted") {
		t.Fatal("should not truncate")
	}
}
