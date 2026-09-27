// Package transcript reads Claude Code session transcripts (.jsonl).
package transcript

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// transcriptLine is one record of a Claude Code session transcript. Only
// user/assistant records are relevant; other record types (attachments,
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

// ReadFile parses a whole transcript.
func ReadFile(path string) ([]Message, error) {
	msgs, _, err := ReadFrom(path, 0)
	return msgs, err
}

// ReadFrom parses the transcript starting at line fromLine (0-based) and
// returns the messages plus the total number of lines, which callers store
// to resume from later. It keeps the conversational text of the main thread
// and drops tool calls/results, thinking blocks, sub-agent (sidechain) turns
// and meta messages, which are noise for summarisation.
func ReadFrom(path string, fromLine int) ([]Message, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fromLine, err
	}
	defer f.Close()
	var msgs []Message
	r := bufio.NewReaderSize(f, 1<<20)
	line := 0
	for {
		b, err := r.ReadBytes('\n')
		if len(b) > 0 {
			complete := err == nil
			if !complete {
				// A partially-written final line: don't consume it.
				break
			}
			if line >= fromLine {
				if m, ok := parseLine(b); ok {
					msgs = append(msgs, m)
				}
			}
			line++
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return msgs, line, err
		}
	}
	return msgs, line, nil
}

func parseLine(b []byte) (Message, bool) {
	var ln transcriptLine
	if json.Unmarshal(b, &ln) != nil {
		return Message{}, false
	}
	if (ln.Type != "user" && ln.Type != "assistant") || ln.IsSidechain || ln.IsMeta {
		return Message{}, false
	}
	text := contentText(ln.Message.Content)
	if text == "" || isNoise(text) {
		return Message{}, false
	}
	return Message{Role: ln.Type, Content: text}, true
}

// isNoise filters harness-generated user messages (command wrappers, hook
// output, interruptions) that carry no knowledge.
func isNoise(s string) bool {
	for _, p := range []string{"<command-name>", "<local-command-stdout>", "<command-message>", "[Request interrupted", "<system-reminder>", "Caveat: The messages below"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
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

// Render formats messages for a prompt, keeping at most budget characters:
// the opening user message (which usually states the task) plus as much of
// the most recent conversation as fits.
func Render(msgs []Message, budget int) string {
	line := func(m Message) string { return m.Role + ": " + m.Content + "\n" }
	var all strings.Builder
	for _, m := range msgs {
		all.WriteString(line(m))
	}
	if all.Len() <= budget || len(msgs) == 0 {
		return all.String()
	}
	head := ""
	if msgs[0].Role == "user" {
		head = line(msgs[0])
		if len(head) > budget/4 {
			head = head[:budget/4] + "…\n"
		}
	}
	remaining := budget - len(head) - 40
	var tail []string
	for i := len(msgs) - 1; i >= 1 && remaining > 0; i-- {
		l := line(msgs[i])
		if len(l) > remaining {
			l = "…" + l[len(l)-remaining:]
		}
		tail = append(tail, l)
		remaining -= len(l)
	}
	var b strings.Builder
	b.WriteString(head)
	b.WriteString("[… earlier conversation omitted …]\n")
	for i := len(tail) - 1; i >= 0; i-- {
		b.WriteString(tail[i])
	}
	return b.String()
}

// Stats summarises a transcript's size.
func Stats(msgs []Message) (users, assistants, chars int) {
	for _, m := range msgs {
		if m.Role == "user" {
			users++
		} else {
			assistants++
		}
		chars += len(m.Content)
	}
	return
}
