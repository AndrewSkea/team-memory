// Package mcpserver exposes team memory to Claude Code (and any MCP client)
// over stdio JSON-RPC.
package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/app"
	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/policy"
	"github.com/AndrewSkea/team-memory/mcp/search"
)

// Supported protocol versions, newest first.
var protocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func strs(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

var readOnly = map[string]any{"readOnlyHint": true, "openWorldHint": false}

// Tools lists the MCP tools.
var Tools = []Tool{
	{
		Name:  "recall",
		Title: "Recall team memory",
		Description: "Search the team's shared memory for knowledge about systems, decisions, gotchas and how-tos — including things " +
			"learned by teammates in other repositories. Use it before investigating an unfamiliar system or when the user asks what the team knows.",
		InputSchema: obj(map[string]any{
			"query": str("What to look for, in natural language or keywords"),
			"about": strs("Optional systems/services/libraries to focus on (e.g. payments-api)"),
			"type":  str("Optional type filter: decision, gotcha, howto, fact, convention, incident, meeting, action, reminder, idea, note"),
			"limit": map[string]any{"type": "integer", "description": "Max results (default 8)"},
		}, "query"),
		Annotations: readOnly,
	},
	{
		Name:        "get_memory",
		Title:       "Get a memory",
		Description: "Fetch the full text of a memory by id (ids appear in recall results and in <team-memory> context).",
		InputSchema: obj(map[string]any{"id": str("Memory id")}, "id"),
		Annotations: readOnly,
	},
	{
		Name:  "remember",
		Title: "Save to team memory",
		Description: "Save knowledge to the team's shared memory so teammates' assistants can recall it. Use when the user asks to remember something, " +
			"or for a durable decision/gotcha/how-to the user agrees is worth sharing. Never include secrets.",
		InputSchema: obj(map[string]any{
			"content": str("The knowledge, written so a teammate can act on it"),
			"title":   str("Short specific title (optional; generated if omitted)"),
			"type":    str("decision, gotcha, howto, fact, convention, incident, meeting, action, reminder, idea or note"),
			"about":   strs("Systems/services/libraries it concerns (optional; inferred if omitted)"),
			"tags":    strs("Keywords (optional)"),
			"scope":   str("team (default) or personal (only recalled for you)"),
		}, "content"),
	},
	{
		Name:        "supersede",
		Title:       "Replace a memory",
		Description: "Publish a corrected version of a memory that is outdated or wrong. The old one stops being recalled.",
		InputSchema: obj(map[string]any{
			"id":      str("Id of the memory being replaced"),
			"content": str("The corrected knowledge"),
			"title":   str("New title (optional)"),
		}, "id", "content"),
	},
	{
		Name:        "forget",
		Title:       "Retract a memory",
		Description: "Retract a memory that is wrong or should not be shared. It stays in git history but is no longer recalled.",
		InputSchema: obj(map[string]any{"id": str("Memory id"), "reason": str("Why it's being retracted")}, "id", "reason"),
		Annotations: map[string]any{"destructiveHint": true},
	},
	{
		Name:        "list_pending",
		Title:       "List memories awaiting review",
		Description: "List automatically captured memories waiting for this user's approval before they are shared with the team.",
		InputSchema: obj(map[string]any{}),
		Annotations: readOnly,
	},
	{
		Name:  "review",
		Title: "Approve or reject a pending memory",
		Description: "Approve (publish to the team, optionally with edits) or reject (discard) a memory waiting for review. " +
			"Only do this when the user has decided.",
		InputSchema: obj(map[string]any{
			"id":      str("Pending item id"),
			"action":  map[string]any{"type": "string", "enum": []string{"approve", "reject"}},
			"title":   str("Edited title (approve only)"),
			"summary": str("Edited summary (approve only)"),
			"content": str("Edited body (approve only)"),
			"about":   strs("Edited systems list (approve only)"),
		}, "id", "action"),
	},
	{
		Name:  "capture_mode",
		Title: "Get or set capture mode",
		Description: "Show or change how automatically captured memories are handled: auto (publish everything), ai (AI decides; uncertain items wait for review), " +
			"review-timeout (wait for review, publish after a few days), review (always wait), manual (no automatic capture). Omit mode to just show it.",
		InputSchema: obj(map[string]any{"mode": str("New mode, or empty to use the team default. Omit to only show the current mode.")}),
	},
	{
		Name:        "lookup",
		Description: "Deprecated alias of recall.",
		InputSchema: obj(map[string]any{"query": str("Search query")}, "query"),
		Annotations: readOnly,
	},
}

type Server struct {
	App     *app.App
	Version string
	// Timeout bounds each tool call.
	Timeout time.Duration
}

// Serve reads JSON-RPC messages from r until EOF.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	if s.Timeout == 0 {
		s.Timeout = 3 * time.Minute
	}
	var wmu sync.Mutex
	enc := json.NewEncoder(w)
	send := func(resp *rpcResponse) {
		wmu.Lock()
		defer wmu.Unlock()
		_ = enc.Encode(resp)
	}
	var wg sync.WaitGroup
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(bytes.TrimPrefix(sc.Bytes(), []byte{0xEF, 0xBB, 0xBF}))
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			send(&rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			continue
		}
		if req.Method == "tools/call" {
			wg.Add(1)
			go func(req rpcRequest) {
				defer wg.Done()
				if resp := s.dispatch(ctx, req); resp != nil {
					send(resp)
				}
			}(req)
			continue
		}
		if resp := s.dispatch(ctx, req); resp != nil {
			send(resp)
		}
	}
	wg.Wait()
	return sc.Err()
}

func isNotification(req rpcRequest) bool { return len(req.ID) == 0 || string(req.ID) == "null" }

func (s *Server) dispatch(ctx context.Context, req rpcRequest) *rpcResponse {
	ok := func(v any) *rpcResponse { return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: v} }
	fail := func(code int, msg string) *rpcResponse {
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{code, msg}}
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := protocolVersions[0]
		for _, v := range protocolVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		return ok(map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "team-memory", "title": "Team Memory", "version": s.Version},
			"instructions": "Team memory holds knowledge captured from teammates' sessions across all the team's repositories. " +
				"Relevant memories are injected automatically in <team-memory> blocks; use recall to search deliberately, " +
				"remember when the user asks to save something, and supersede/forget when a memory proves wrong.",
		})
	case "ping":
		return ok(map[string]any{})
	case "tools/list":
		return ok(map[string]any{"tools": Tools})
	case "resources/list":
		return ok(map[string]any{"resources": []any{}})
	case "prompts/list":
		return ok(map[string]any{"prompts": []any{}})
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fail(-32602, "invalid params")
		}
		cctx, cancel := context.WithTimeout(ctx, s.Timeout)
		defer cancel()
		text, err := s.call(cctx, p.Name, p.Arguments)
		if err == errUnknownTool {
			return fail(-32602, "unknown tool: "+p.Name)
		}
		if err != nil {
			return ok(map[string]any{"content": []map[string]any{{"type": "text", "text": "Error: " + err.Error()}}, "isError": true})
		}
		return ok(map[string]any{"content": []map[string]any{{"type": "text", "text": text}}})
	}
	if isNotification(req) {
		return nil
	}
	return fail(-32601, "method not found: "+req.Method)
}

var errUnknownTool = fmt.Errorf("unknown tool")

type toolArgs struct {
	Query   string   `json:"query"`
	About   []string `json:"about"`
	Type    string   `json:"type"`
	Limit   int      `json:"limit"`
	ID      string   `json:"id"`
	Content string   `json:"content"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Tags    []string `json:"tags"`
	Scope   string   `json:"scope"`
	Reason  string   `json:"reason"`
	Action  string   `json:"action"`
	Mode    *string  `json:"mode"`
}

func (s *Server) call(ctx context.Context, name string, raw json.RawMessage) (string, error) {
	var a toolArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", fmt.Errorf("invalid arguments: %v", err)
		}
	}
	switch name {
	case "recall", "lookup":
		if strings.TrimSpace(a.Query) == "" && len(a.About) == 0 {
			return "", fmt.Errorf("query is required")
		}
		if a.Limit <= 0 || a.Limit > 50 {
			a.Limit = 8
		}
		s.App.EnsureFresh(ctx, 2*time.Minute)
		rs, err := s.App.Search(app.SearchInput{Query: a.Query, About: a.About, Type: a.Type, Limit: a.Limit})
		if err != nil {
			return "", err
		}
		return formatResults(s.App, rs, a.Query), nil
	case "get_memory":
		m, err := s.App.Get(strings.TrimSpace(a.ID))
		if err != nil {
			return "", err
		}
		return formatFull(s.App, m), nil
	case "remember":
		m, warns, err := s.App.Remember(ctx, app.RememberInput{Title: a.Title, Text: a.Content, Type: a.Type, About: a.About,
			Tags: a.Tags, Scope: a.Scope, Source: "mcp", Classify: true})
		if err != nil {
			return "", err
		}
		return savedMsg(s.App, "Saved", m, warns), nil
	case "supersede":
		m, warns, err := s.App.Supersede(ctx, a.ID, app.RememberInput{Title: a.Title, Text: a.Content, Source: "mcp", Classify: true})
		if err != nil {
			return "", err
		}
		return savedMsg(s.App, "Saved replacement", m, warns) + fmt.Sprintf("\n%s is now marked superseded.", a.ID), nil
	case "forget":
		m, err := s.App.Forget(ctx, a.ID, a.Reason)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Retracted %q (%s). It will no longer be recalled.", m.Title, m.ID), nil
	case "list_pending":
		items, err := s.App.ListPending()
		if err != nil {
			return "", err
		}
		if len(items) == 0 {
			return "Nothing is waiting for review.", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d item(s) waiting for review:\n\n", len(items))
		for _, p := range items {
			m := p.Memory
			fmt.Fprintf(&b, "## %s\nid: %s\nkind: %s · type: %s · about: %s\nwhy held: %s\n", m.Title, p.ID(), p.Kind, m.Type,
				strings.Join(m.About, ", "), p.Reason)
			if m.Summary != "" {
				fmt.Fprintf(&b, "summary: %s\n", m.Summary)
			}
			if m.Body != "" {
				fmt.Fprintf(&b, "%s\n", m.Body)
			}
			b.WriteString("\n")
		}
		return b.String(), nil
	case "review":
		switch a.Action {
		case "reject":
			if err := s.App.Reject(a.ID); err != nil {
				return "", err
			}
			return "Rejected and discarded " + a.ID + ".", nil
		case "approve":
			var e app.Edits
			if a.Title != "" {
				e.Title = &a.Title
			}
			if a.Summary != "" {
				e.Summary = &a.Summary
			}
			if a.Content != "" {
				e.Body = &a.Content
			}
			if len(a.About) > 0 {
				e.About = &a.About
			}
			m, err := s.App.Approve(ctx, a.ID, &e)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Approved and published %q (%s).", m.Title, m.ID), nil
		}
		return "", fmt.Errorf("action must be approve or reject")
	case "capture_mode":
		if a.Mode != nil {
			eff, err := s.App.SetMode(strings.TrimSpace(*a.Mode))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Capture mode is now %s: %s", eff, policy.Descriptions[eff]), nil
		}
		st := s.App.Settings()
		var b strings.Builder
		fmt.Fprintf(&b, "Current capture mode: %s — %s\n", st.EffectiveMode, policy.Descriptions[st.EffectiveMode])
		fmt.Fprintf(&b, "Team default: %s · team minimum: %s\nAvailable modes:\n", st.TeamDefault, st.TeamMinimum)
		for _, m := range st.AllowedModes {
			fmt.Fprintf(&b, "- %s: %s\n", m, policy.Descriptions[m])
		}
		return b.String(), nil
	}
	return "", errUnknownTool
}

func formatResults(a *app.App, rs []search.Result, q string) string {
	if len(rs) == 0 {
		return fmt.Sprintf("No team memories found for %q.", q)
	}
	var b strings.Builder
	for i, r := range rs {
		m := r.Memory
		fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, m.ID, m.Title)
		var meta []string
		if m.Type != "" {
			meta = append(meta, m.Type)
		}
		if len(m.About) > 0 {
			meta = append(meta, "about: "+strings.Join(m.About, ", "))
		}
		if m.LearnedIn != "" {
			meta = append(meta, "learned in "+m.LearnedIn)
		}
		if m.Author != "" {
			meta = append(meta, "by "+m.Author)
		}
		if !m.Created.IsZero() {
			meta = append(meta, app.Ago(a.Now(), m.Created))
		}
		if !m.Active() {
			meta = append(meta, "STATUS: "+m.Status)
		}
		fmt.Fprintf(&b, "   %s\n", strings.Join(meta, " · "))
		if m.Summary != "" {
			fmt.Fprintf(&b, "   %s\n", m.Summary)
		}
	}
	b.WriteString("\nUse get_memory with an id for full details.")
	return b.String()
}

func formatFull(a *app.App, m memory.Memory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\nid: %s\ntype: %s\n", m.Title, m.ID, m.Type)
	if len(m.About) > 0 {
		fmt.Fprintf(&b, "about: %s\n", strings.Join(m.About, ", "))
	}
	if len(m.Tags) > 0 {
		fmt.Fprintf(&b, "tags: %s\n", strings.Join(m.Tags, ", "))
	}
	if m.LearnedIn != "" || m.Author != "" {
		fmt.Fprintf(&b, "learned in: %s by %s\n", m.LearnedIn, m.Author)
	}
	if !m.Created.IsZero() {
		fmt.Fprintf(&b, "created: %s (%s)\n", m.Created.Format("2006-01-02"), app.Ago(a.Now(), m.Created))
	}
	if !m.Active() {
		fmt.Fprintf(&b, "STATUS: %s %s %s\n", m.Status, m.SupersededBy, m.StatusReason)
	}
	fmt.Fprintf(&b, "file: %s\n\n", m.Path)
	if m.Summary != "" {
		fmt.Fprintf(&b, "%s\n\n", m.Summary)
	}
	b.WriteString(m.Body)
	return b.String()
}

func savedMsg(a *app.App, verb string, m memory.Memory, warns []string) string {
	s := fmt.Sprintf("%s to team memory (%s): %q [%s]", verb, a.Config().Slug(), m.Title, m.ID)
	if len(m.About) > 0 {
		s += "\nAbout: " + strings.Join(m.About, ", ")
	}
	for _, w := range warns {
		s += "\nNote: " + w
	}
	return s
}
