package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/internal/testenv"
)

type resp struct {
	ID     json.RawMessage `json:"id"`
	Result struct {
		ProtocolVersion string `json:"protocolVersion"`
		Tools           []Tool `json:"tools"`
		Content         []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	} `json:"result"`
	Error *rpcError `json:"error"`
}

func run(t *testing.T, s *Server, lines ...string) map[string]resp {
	t.Helper()
	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]resp{}
	dec := json.NewDecoder(&out)
	for dec.More() {
		var r resp
		if err := dec.Decode(&r); err != nil {
			t.Fatal(err)
		}
		got[string(r.ID)] = r
	}
	return got
}

func call(id int, tool string, args any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args}})
	return string(b)
}

func text(r resp) string {
	if len(r.Result.Content) == 0 {
		return ""
	}
	return r.Result.Content[0].Text
}

func TestProtocol(t *testing.T) {
	env := testenv.New(t, nil, false)
	s := &Server{App: env.App, Version: "1.2.3"}
	got := run(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":4,"method":"bogus"}`,
		`{"jsonrpc":"2.0","method":"bogus/notification"}`,
		`not json`,
		`{"jsonrpc":"2.0","id":"s5","method":"initialize","params":{"protocolVersion":"2099-01-01"}}`,
		call(6, "nope", map[string]any{}),
		call(7, "recall", map[string]any{"query": "x"}),
	)
	if got["1"].Result.ProtocolVersion != "2025-03-26" {
		t.Errorf("negotiation: %+v", got["1"])
	}
	if got[`"s5"`].Result.ProtocolVersion != protocolVersions[0] {
		t.Errorf("unknown version fallback: %+v", got[`"s5"`])
	}
	if len(got["2"].Result.Tools) != len(Tools) {
		t.Errorf("tools/list: %d", len(got["2"].Result.Tools))
	}
	if got["3"].Error != nil || got["4"].Error == nil || got["4"].Error.Code != -32601 {
		t.Errorf("ping/unknown: %+v %+v", got["3"], got["4"])
	}
	if got["null"].Error == nil || got["null"].Error.Code != -32700 {
		t.Errorf("parse error: %+v", got["null"])
	}
	if got["6"].Error == nil {
		t.Error("unknown tool should be a protocol error")
	}
	// Unconfigured: tool error explains how to set up.
	if !got["7"].Result.IsError || !strings.Contains(text(got["7"]), "not set up") {
		t.Errorf("unconfigured: %+v", got["7"])
	}
	if len(got) != 8 {
		t.Errorf("notifications must not get responses; got %d responses", len(got))
	}
}

func TestToolsEndToEnd(t *testing.T) {
	env := testenv.New(t, nil, true)
	s := &Server{App: env.App, Version: "test"}
	got := run(t, s,
		call(1, "remember", map[string]any{"content": "The payments API rate-limits at 100 rps per key. Retry with jitter.", "about": []string{"payments-api"}, "type": "gotcha"}),
		call(2, "remember", map[string]any{}),
	)
	if got["1"].Result.IsError || !strings.Contains(text(got["1"]), "Saved to team memory (acme/kb)") {
		t.Fatalf("remember: %+v", got["1"])
	}
	if !got["2"].Result.IsError {
		t.Fatal("empty remember should error")
	}
	id := text(got["1"])
	id = id[strings.LastIndex(id, "[")+1 : strings.LastIndex(id, "]")]

	got = run(t, s,
		call(3, "recall", map[string]any{"query": "payments rate limit"}),
		call(4, "get_memory", map[string]any{"id": id}),
		call(5, "lookup", map[string]any{"query": "payments"}),
		call(6, "recall", map[string]any{"query": "zebras"}),
		call(10, "list_pending", map[string]any{}),
	)
	// capture_mode calls depend on each other; tools/call runs concurrently.
	for k, v := range run(t, s, call(7, "capture_mode", map[string]any{})) {
		got[k] = v
	}
	for k, v := range run(t, s, call(8, "capture_mode", map[string]any{"mode": "review"}), call(9, "capture_mode", map[string]any{"mode": "yolo"})) {
		got[k] = v
	}
	if !strings.Contains(text(got["3"]), id) || !strings.Contains(text(got["5"]), id) {
		t.Fatalf("recall: %s", text(got["3"]))
	}
	if !strings.Contains(text(got["4"]), "about: payments-api") {
		t.Fatalf("get_memory: %s", text(got["4"]))
	}
	if !strings.Contains(text(got["6"]), "No team memories") {
		t.Fatalf("empty recall: %s", text(got["6"]))
	}
	if !strings.Contains(text(got["7"]), "Current capture mode: ai") || !strings.Contains(text(got["8"]), "now review") || !got["9"].Result.IsError {
		t.Fatalf("capture_mode: %q %q %+v", text(got["7"]), text(got["8"]), got["9"])
	}
	if !strings.Contains(text(got["10"]), "Nothing is waiting") {
		t.Fatalf("list_pending: %s", text(got["10"]))
	}

	got = run(t, s,
		call(11, "supersede", map[string]any{"id": id, "content": "The limit is now 500 rps per key."}),
		call(12, "review", map[string]any{"id": "nope", "action": "approve"}),
	)
	if got["11"].Result.IsError || !strings.Contains(text(got["11"]), "superseded") {
		t.Fatalf("supersede: %s", text(got["11"]))
	}
	if !got["12"].Result.IsError {
		t.Fatal("review of unknown id should error")
	}
	newID := text(got["11"])
	newID = newID[strings.Index(newID, "[")+1 : strings.Index(newID, "]")]
	// tools/call runs concurrently, so sequence dependent calls.
	forgot := run(t, s, call(13, "forget", map[string]any{"id": newID, "reason": "test"}))
	after := run(t, s, call(14, "recall", map[string]any{"query": "payments limit rps"}))
	if forgot["13"].Result.IsError || !strings.Contains(text(after["14"]), "No team memories") {
		t.Fatalf("forget: %s / %s", text(forgot["13"]), text(after["14"]))
	}
}
