package hook

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// transcriptLine is one record of a Claude Code session transcript (.jsonl).
// Only user/assistant records are relevant; other record types (attachments,
// mode changes, file-history snapshots, …) are skipped.
type transcriptLine struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	Message     struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// ReadTranscriptFile parses the JSONL transcript Claude Code passes to hooks
// via `transcript_path`. It keeps the conversational text of the main thread
// and drops tool calls/results, thinking blocks, sub-agent (sidechain) turns
// and meta messages, which are noise for summarisation.
func ReadTranscriptFile(path string) ([]Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var msgs []Message
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		var ln transcriptLine
		if json.Unmarshal(sc.Bytes(), &ln) != nil {
			continue
		}
		if (ln.Type != "user" && ln.Type != "assistant") || ln.IsSidechain || ln.IsMeta {
			continue
		}
		text := contentText(ln.Message.Content)
		if text == "" {
			continue
		}
		msgs = append(msgs, Message{Role: ln.Type, Content: text})
	}
	return msgs, sc.Err()
}

// contentText returns the text of a message content field, which is either a
// plain string or a list of typed blocks.
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, strings.TrimSpace(b.Text))
		}
	}
	return strings.Join(parts, "\n")
}
