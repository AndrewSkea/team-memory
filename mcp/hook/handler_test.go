package hook_test

import (
	"context"
	"strings"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/config"
	"github.com/AndrewSkea/team-memory/mcp/hook"
)

type fakeRunner struct{ out string }

func (f *fakeRunner) Run(_ context.Context, _ string) (string, error) { return f.out, nil }

type fakeGH struct {
	indexContent string
	committed    []string
	putPaths     []string
}

func (g *fakeGH) GetFile(_ context.Context, path string) (hook.FileInfo, error) {
	if path == "INDEX.md" {
		return hook.FileInfo{Content: g.indexContent, Exists: true, SHA: "abc"}, nil
	}
	return hook.FileInfo{Exists: false}, nil
}

func (g *fakeGH) CommitFile(_ context.Context, path, appendContent, msg string) error {
	g.committed = append(g.committed, path)
	return nil
}

func (g *fakeGH) PutContent(_ context.Context, path, content, msg string) error {
	g.putPaths = append(g.putPaths, path)
	return nil
}

func TestRunSessionEndHappyPath(t *testing.T) {
	runner := &fakeRunner{out: `{
		"target_file": "GENERAL.md",
		"short_title": "tested the thing",
		"one_sentence_summary": "We tested stuff.",
		"bullets": ["wrote tests", "fixed bug"],
		"tags": "testing;go",
		"unsure": false
	}`}
	gh := &fakeGH{indexContent: "GENERAL.md | shared | general\n"}
	transcript := []hook.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	err := hook.RunSessionEnd(context.Background(), config.Config{}, transcript, runner, gh)
	if err != nil {
		t.Fatal(err)
	}
	if len(gh.committed) != 1 || gh.committed[0] != "GENERAL.md" {
		t.Fatalf("expected commit to GENERAL.md, got %v", gh.committed)
	}
}

func TestRunSessionEndEmptyTranscript(t *testing.T) {
	runner := &fakeRunner{}
	gh := &fakeGH{}
	err := hook.RunSessionEnd(context.Background(), config.Config{}, nil, runner, gh)
	if err != nil {
		t.Fatal(err)
	}
	if len(gh.committed) != 0 {
		t.Fatal("should not commit for empty transcript")
	}
}

func TestRunSessionEndNewFileUpdatesIndex(t *testing.T) {
	runner := &fakeRunner{out: `{
		"target_file": "new-topic.md",
		"short_title": "new topic",
		"one_sentence_summary": "Created new topic.",
		"bullets": ["a"],
		"tags": "new",
		"unsure": false
	}`}
	gh := &fakeGH{indexContent: "GENERAL.md | shared | general\n"}
	transcript := []hook.Message{{Role: "user", Content: "content"}}
	err := hook.RunSessionEnd(context.Background(), config.Config{}, transcript, runner, gh)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(gh.putPaths, ","), "INDEX.md") {
		t.Fatal("expected INDEX.md update")
	}
}

func TestTruncateTranscript(t *testing.T) {
	msgs := make([]hook.Message, 100)
	for i := range msgs {
		msgs[i] = hook.Message{Role: "user", Content: "msg"}
	}
	got := hook.TruncateTranscript(msgs, 80)
	if len(got) != 80 {
		t.Fatalf("expected 80, got %d", len(got))
	}
	if got[0] != msgs[20] {
		t.Fatal("expected last 80 messages")
	}
}
