package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type streamEvent struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype"`
	IsError bool            `json:"is_error"`
	Result  string          `json:"result"`
	Message json.RawMessage `json:"message"`
}

type assistantMessage struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// ExtractAssistantText reads stream-json (one event per line) and returns the
// concatenated text content of all assistant messages. Returns error if the
// stream contains an error result event.
func ExtractAssistantText(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var b strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev streamEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return "", fmt.Errorf("invalid stream-json line: %w", err)
		}
		switch ev.Type {
		case "assistant":
			var msg assistantMessage
			if err := json.Unmarshal(ev.Message, &msg); err != nil {
				return "", fmt.Errorf("invalid assistant message: %w", err)
			}
			for _, c := range msg.Content {
				if c.Type == "text" {
					b.WriteString(c.Text)
				}
			}
		case "result":
			if ev.IsError {
				return "", fmt.Errorf("claude error: %s", ev.Result)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return b.String(), nil
}
