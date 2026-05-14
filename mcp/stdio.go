package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/config"
	gh "github.com/AndrewSkea/team-memory/mcp/github"
	"github.com/AndrewSkea/team-memory/mcp/hook"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

var mcpTools = []mcpTool{
	{
		Name:        "remember",
		Description: "Save a memory entry to the team memory repository on GitHub",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "The memory content to save",
				},
				"title": map[string]any{
					"type":        "string",
					"description": "Short title for the entry",
				},
				"file": map[string]any{
					"type":        "string",
					"description": "Target memory file (e.g. GENERAL.md). Defaults to GENERAL.md",
				},
			},
			"required": []string{"content", "title"},
		},
	},
	{
		Name:        "lookup",
		Description: "Search team memory for entries matching a keyword or phrase",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query",
				},
			},
			"required": []string{"query"},
		},
	},
}

func runMCPStdio() error {
	cfg, err := config.Read(config.DefaultPath())
	if err != nil && !config.IsMissing(err) {
		return err
	}
	client := gh.NewClient(cfg, "")

	enc := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		line := bytes.TrimPrefix(scanner.Bytes(), []byte{0xEF, 0xBB, 0xBF}) // strip UTF-8 BOM
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			log.Printf("mcp stdio: dropping malformed JSON-RPC frame: %v", err)
			continue
		}
		resp := dispatchMCP(req, cfg, client)
		if resp != nil {
			_ = enc.Encode(resp)
		}
	}
	return scanner.Err()
}

func dispatchMCP(req rpcRequest, cfg config.Config, client *gh.Client) *rpcResponse {
	switch req.Method {
	case "initialize":
		return &rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "team-memory", "version": "1.0.0"},
			},
		}
	case "notifications/initialized":
		return nil
	case "ping":
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	case "tools/list":
		return &rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"tools": mcpTools},
		}
	case "tools/call":
		return handleToolCall(req, cfg, client)
	default:
		if req.ID != nil {
			return &rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &rpcError{Code: -32601, Message: "method not found: " + req.Method},
			}
		}
		return nil
	}
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func handleToolCall(req rpcRequest, cfg config.Config, client *gh.Client) *rpcResponse {
	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return mcpErrResp(req.ID, -32602, "invalid params")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var text string
	var toolErr error

	switch params.Name {
	case "remember":
		text, toolErr = toolRemember(ctx, client, params.Arguments)
	case "lookup":
		text, toolErr = toolLookup(ctx, client, params.Arguments)
	default:
		return mcpErrResp(req.ID, -32602, "unknown tool: "+params.Name)
	}

	if toolErr != nil {
		return &rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{{"type": "text", "text": "Error: " + toolErr.Error()}},
				"isError": true,
			},
		}
	}
	return &rpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
		},
	}
}

func toolRemember(ctx context.Context, client *gh.Client, args map[string]any) (string, error) {
	content, _ := args["content"].(string)
	if content == "" {
		return "", fmt.Errorf("content is required")
	}
	title, _ := args["title"].(string)
	if title == "" {
		title = "Manual entry"
	}
	file, _ := args["file"].(string)
	if file == "" {
		file = "GENERAL.md"
	}

	entry := hook.Entry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		ShortTitle: title,
		Scope:      "Team",
		Type:       "General",
		Source:     "MCP",
		Summary:    content,
		Full:       content,
	}
	block := "\n" + hook.RenderEntry(entry)

	if err := client.CommitFile(ctx, file, block, "team-memory: add entry to "+file); err != nil {
		return "", err
	}
	return fmt.Sprintf("Saved to %s", file), nil
}

func toolLookup(ctx context.Context, client *gh.Client, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	idx, err := client.GetFile(ctx, "INDEX.md")
	if err != nil || !idx.Exists {
		return "No memories found (INDEX.md missing)", nil
	}

	var files []string
	for _, line := range strings.Split(idx.Content, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 2)
		if len(parts) >= 1 {
			name := strings.TrimSpace(parts[0])
			if strings.HasSuffix(name, ".md") && name != "INDEX.md" {
				files = append(files, name)
			}
		}
	}

	queryLower := strings.ToLower(query)
	var results []string

	for _, f := range files {
		info, err := client.GetFile(ctx, f)
		if err != nil || !info.Exists {
			continue
		}
		for _, block := range splitEntries(info.Content) {
			if strings.Contains(strings.ToLower(block), queryLower) {
				results = append(results, fmt.Sprintf("[%s]\n%s", f, block))
			}
		}
	}

	if len(results) == 0 {
		return fmt.Sprintf("No memories found matching %q", query), nil
	}
	return strings.Join(results, "\n\n---\n\n"), nil
}

func splitEntries(content string) []string {
	var entries []string
	var cur strings.Builder
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "### Entry:") && cur.Len() > 0 {
			entries = append(entries, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
	}
	if cur.Len() > 0 {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			entries = append(entries, s)
		}
	}
	return entries
}

func mcpErrResp(id any, code int, msg string) *rpcResponse {
	return &rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	}
}
