package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// APIModel is the Messages API model used when the claude CLI is absent.
const APIModel = "claude-haiku-4-5"

// API calls the Anthropic Messages API directly.
type API struct {
	key, model, url string
	http            *http.Client
}

func NewAPI(key string) *API {
	model := os.Getenv("TEAM_MEMORY_API_MODEL")
	if model == "" {
		model = APIModel
	}
	u := os.Getenv("ANTHROPIC_BASE_URL")
	if u == "" {
		u = "https://api.anthropic.com"
	}
	return &API{key: key, model: model, url: strings.TrimRight(u, "/") + "/v1/messages",
		http: &http.Client{Timeout: 120 * time.Second}}
}

func (a *API) Run(ctx context.Context, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":      a.model,
		"max_tokens": 4096,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", a.key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("anthropic API: HTTP %d: %.300s", resp.StatusCode, data)
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String(), nil
}
