package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/config"
	gh "github.com/AndrewSkea/team-memory/mcp/github"
)

func newTestMCPDeps() (config.Config, *gh.Client) {
	cfg := config.Config{Token: "test", Owner: "test", Repo: "test"}
	client := gh.NewClient(cfg, "")
	return cfg, client
}

func TestMCPInitialize(t *testing.T) {
	cfg, client := newTestMCPDeps()
	req := rpcRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"}
	resp := dispatchMCP(req, cfg, client)

	if resp == nil {
		t.Fatal("got nil response")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not map: %T", resp.Result)
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want 2024-11-05", result["protocolVersion"])
	}
	info, _ := result["serverInfo"].(map[string]any)
	if info["name"] != "team-memory" {
		t.Errorf("serverInfo.name = %v, want team-memory", info["name"])
	}
}

func TestMCPPing(t *testing.T) {
	cfg, client := newTestMCPDeps()
	req := rpcRequest{JSONRPC: "2.0", ID: 2, Method: "ping"}
	resp := dispatchMCP(req, cfg, client)

	if resp == nil {
		t.Fatal("got nil response")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
}

func TestMCPToolsList(t *testing.T) {
	cfg, client := newTestMCPDeps()
	req := rpcRequest{JSONRPC: "2.0", ID: 3, Method: "tools/list"}
	resp := dispatchMCP(req, cfg, client)

	if resp == nil {
		t.Fatal("got nil response")
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not map: %T", resp.Result)
	}
	tools, ok := result["tools"].([]mcpTool)
	if !ok {
		t.Fatalf("tools is not []mcpTool: %T", result["tools"])
	}
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"remember", "lookup"} {
		if !names[want] {
			t.Errorf("missing tool %q in tools list", want)
		}
	}
}

func TestMCPNotificationsInitialized(t *testing.T) {
	cfg, client := newTestMCPDeps()
	// notifications/initialized has no ID and should return nil (fire-and-forget)
	req := rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}
	resp := dispatchMCP(req, cfg, client)
	if resp != nil {
		t.Errorf("notifications/initialized should return nil, got %+v", resp)
	}
}

func TestMCPUnknownMethodWithID(t *testing.T) {
	cfg, client := newTestMCPDeps()
	req := rpcRequest{JSONRPC: "2.0", ID: 99, Method: "unknown/method"}
	resp := dispatchMCP(req, cfg, client)

	if resp == nil {
		t.Fatal("got nil for unknown method with ID")
	}
	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestMCPUnknownMethodNoID(t *testing.T) {
	cfg, client := newTestMCPDeps()
	req := rpcRequest{JSONRPC: "2.0", Method: "unknown/notification"}
	resp := dispatchMCP(req, cfg, client)
	// No ID = notification, should silently ignore
	if resp != nil {
		t.Errorf("unknown notification with no ID should return nil, got %+v", resp)
	}
}

func TestMCPToolCallMissingContent(t *testing.T) {
	cfg, client := newTestMCPDeps()
	params, _ := json.Marshal(map[string]any{
		"name":      "remember",
		"arguments": map[string]any{},
	})
	req := rpcRequest{JSONRPC: "2.0", ID: 4, Method: "tools/call", Params: params}
	resp := dispatchMCP(req, cfg, client)

	if resp == nil {
		t.Fatal("got nil response")
	}
	// Should return isError:true result (not an RPC error)
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not map: %T", resp.Result)
	}
	if result["isError"] != true {
		t.Errorf("expected isError:true for missing content, got %v", result["isError"])
	}
}

func TestMCPToolCallUnknown(t *testing.T) {
	cfg, client := newTestMCPDeps()
	params, _ := json.Marshal(map[string]any{
		"name":      "nonexistent-tool",
		"arguments": map[string]any{},
	})
	req := rpcRequest{JSONRPC: "2.0", ID: 5, Method: "tools/call", Params: params}
	resp := dispatchMCP(req, cfg, client)

	if resp == nil {
		t.Fatal("got nil response")
	}
	if resp.Error == nil {
		t.Fatal("expected RPC error for unknown tool")
	}
	if !strings.Contains(resp.Error.Message, "nonexistent-tool") {
		t.Errorf("error message = %q, should mention tool name", resp.Error.Message)
	}
}

func TestMCPLookupNoQuery(t *testing.T) {
	cfg, client := newTestMCPDeps()
	params, _ := json.Marshal(map[string]any{
		"name":      "lookup",
		"arguments": map[string]any{"query": ""},
	})
	req := rpcRequest{JSONRPC: "2.0", ID: 6, Method: "tools/call", Params: params}
	resp := dispatchMCP(req, cfg, client)

	if resp == nil {
		t.Fatal("got nil response")
	}
	result, _ := resp.Result.(map[string]any)
	if result["isError"] != true {
		t.Errorf("empty query should return isError:true, got %v", result)
	}
}

func TestSplitEntries(t *testing.T) {
	content := `### Entry: 2026-01-01 | first
body of first

### Entry: 2026-01-02 | second
body of second
`
	entries := splitEntries(content)
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d: %v", len(entries), entries)
	}
	if !strings.Contains(entries[0], "first") {
		t.Errorf("entry[0] missing 'first': %q", entries[0])
	}
	if !strings.Contains(entries[1], "second") {
		t.Errorf("entry[1] missing 'second': %q", entries[1])
	}
}

func TestSplitEntriesEmpty(t *testing.T) {
	entries := splitEntries("")
	if len(entries) != 0 {
		t.Errorf("empty input: want 0 entries, got %d", len(entries))
	}
}
