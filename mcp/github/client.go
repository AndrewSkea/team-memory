package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/AndrewSkea/team-memory/mcp/config"
)

type FileInfo struct {
	SHA     string
	Content string
	Exists  bool
}

type Client struct {
	cfg     config.Config
	baseURL string
	http    *http.Client
}

func NewClient(cfg config.Config, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return &Client{cfg: cfg, baseURL: baseURL, http: &http.Client{}}
}

func (c *Client) repoURL(path string) string {
	return fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.baseURL, c.cfg.Owner, c.cfg.Repo, path)
}

func (c *Client) do(ctx context.Context, method, url string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

func (c *Client) GetFile(ctx context.Context, path string) (FileInfo, error) {
	resp, err := c.do(ctx, http.MethodGet, c.repoURL(path), nil)
	if err != nil {
		return FileInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return FileInfo{Exists: false}, nil
	}
	if resp.StatusCode >= 400 {
		return FileInfo{}, fmt.Errorf("github GET %s: status %d", path, resp.StatusCode)
	}
	var obj struct {
		SHA     string `json:"sha"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		return FileInfo{}, err
	}
	raw := strings.ReplaceAll(obj.Content, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return FileInfo{}, fmt.Errorf("base64 decode: %w", err)
	}
	return FileInfo{SHA: obj.SHA, Content: string(decoded), Exists: true}, nil
}

func (c *Client) CommitFile(ctx context.Context, path, appendContent, message string) error {
	for attempt := range 3 {
		f, err := c.GetFile(ctx, path)
		if err != nil {
			return err
		}
		content := f.Content + appendContent
		body := map[string]any{
			"message": message,
			"content": base64.StdEncoding.EncodeToString([]byte(content)),
		}
		if f.Exists {
			body["sha"] = f.SHA
		}
		resp, err := c.do(ctx, http.MethodPut, c.repoURL(path), body)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode == 409 {
			if attempt == 2 {
				return fmt.Errorf("team-memory: conflict writing %s after 3 retries", path)
			}
			continue
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("github PUT %s: status %d", path, resp.StatusCode)
		}
		return nil
	}
	return fmt.Errorf("team-memory: conflict writing %s after 3 retries", path)
}

func (c *Client) PutContent(ctx context.Context, path, content, message string) error {
	f, err := c.GetFile(ctx, path)
	if err != nil {
		return err
	}
	body := map[string]any{
		"message": message,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
	}
	if f.Exists {
		body["sha"] = f.SHA
	}
	resp, err := c.do(ctx, http.MethodPut, c.repoURL(path), body)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("github PUT %s: status %d", path, resp.StatusCode)
	}
	return nil
}
