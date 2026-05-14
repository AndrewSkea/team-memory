package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/config"
	"github.com/AndrewSkea/team-memory/mcp/github"
	"github.com/AndrewSkea/team-memory/mcp/prompts"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type FileInfo struct {
	SHA     string
	Content string
	Exists  bool
}

type Runner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

type GitHubClient interface {
	GetFile(ctx context.Context, path string) (FileInfo, error)
	CommitFile(ctx context.Context, path, appendContent, message string) error
	PutContent(ctx context.Context, path, content, message string) error
}

type categorizeResult struct {
	TargetFile         string   `json:"target_file"`
	ShortTitle         string   `json:"short_title"`
	OneSentenceSummary string   `json:"one_sentence_summary"`
	Bullets            []string `json:"bullets"`
	Tags               string   `json:"tags"`
	Unsure             bool     `json:"unsure"`
}

func TruncateTranscript(msgs []Message, max int) []Message {
	if len(msgs) <= max {
		return msgs
	}
	return msgs[len(msgs)-max:]
}

func transcriptText(msgs []Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

func runOnce(ctx context.Context, cfg config.Config, transcript []Message, source string, runner Runner, gh GitHubClient) error {
	if len(transcript) == 0 {
		return nil
	}
	transcript = TruncateTranscript(transcript, 80)

	idxFile, err := gh.GetFile(ctx, "INDEX.md")
	if err != nil {
		return fmt.Errorf("fetch INDEX.md: %w", err)
	}
	indexContent := idxFile.Content

	prompt := prompts.Session +
		"\n\nTRANSCRIPT:\n" + transcriptText(transcript) +
		"\n\nINDEX:\n" + indexContent

	out, err := runner.Run(ctx, prompt)
	if err != nil {
		return err
	}
	out = strings.TrimSpace(out)
	if strings.HasPrefix(out, "```") {
		if i := strings.Index(out, "\n"); i >= 0 {
			out = out[i+1:]
		}
		if j := strings.LastIndex(out, "```"); j >= 0 {
			out = out[:j]
		}
		out = strings.TrimSpace(out)
	}

	var result categorizeResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return fmt.Errorf("parse claude output: %w (raw: %.300s)", err, out)
	}

	target := result.TargetFile
	if result.Unsure || target == "" {
		target = "UNSURE.md"
	}
	if err := github.ValidatePath(target); err != nil {
		// LLM returned a malformed/dangerous path — fall back to UNSURE.md.
		target = "UNSURE.md"
	}

	entry := Entry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		ShortTitle: result.ShortTitle,
		Scope:      "Team",
		Type:       "General",
		Tags:       result.Tags,
		Source:     source,
		Summary:    result.OneSentenceSummary,
		Bullets:    result.Bullets,
		Full:       result.OneSentenceSummary,
	}
	block := "\n" + RenderEntry(entry)

	if err := gh.CommitFile(ctx, target, block, "team-memory: add entry to "+target); err != nil {
		return err
	}

	// Update INDEX if target not listed
	if !strings.Contains(indexContent, target) {
		newIndex := strings.TrimRight(indexContent, "\n") + "\n" + target + " | shared | general\n"
		if err := gh.PutContent(ctx, "INDEX.md", newIndex, "team-memory: update INDEX.md"); err != nil {
			return err
		}
	}
	return nil
}

func RunSessionEnd(ctx context.Context, cfg config.Config, transcript []Message, runner Runner, gh GitHubClient) error {
	return runOnce(ctx, cfg, transcript, "Stop", runner, gh)
}

func RunPreCompact(ctx context.Context, cfg config.Config, transcript []Message, runner Runner, gh GitHubClient) error {
	return runOnce(ctx, cfg, transcript, "PreCompact", runner, gh)
}
