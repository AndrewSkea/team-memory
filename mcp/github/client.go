package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/config"
)

// validRepoPath restricts writable paths to repo-relative markdown segments.
// Allowed: [A-Za-z0-9_-] in each segment, separated by "/", ending in ".md".
// Rejects "..", leading "/", spaces, and dots inside segment names.
var validRepoPath = regexp.MustCompile(`^(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9_-]+\.md$`)

// metaPaths are the non-markdown files team-memory itself maintains.
var metaPaths = map[string]bool{
	".team-memory/entities.yml": true,
	".team-memory/policy.yml":   true,
}

func ValidatePath(path string) error {
	if metaPaths[path] {
		return nil
	}
	if !validRepoPath.MatchString(path) {
		return fmt.Errorf("invalid repo path: %q", path)
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed: %q", path)
	}
	return nil
}

type FileInfo struct {
	SHA     string
	Content string
	Exists  bool
}

// ErrConflict means the file changed (or already exists) since it was read.
var ErrConflict = errors.New("github: file changed concurrently")

// ErrEmptyRepo means the repository has no commits yet.
var ErrEmptyRepo = errors.New("github: repository is empty")

// StatusError is a non-2xx GitHub response.
type StatusError struct {
	Op      string
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	hint := ""
	switch e.Status {
	case 401:
		hint = " — token is invalid or expired"
	case 403:
		hint = " — token lacks permission (needs Contents: read & write on the memory repo) or rate limited"
	case 404:
		hint = " — repo not found, or the token cannot see it"
	}
	msg := ""
	if e.Message != "" {
		msg = ": " + e.Message
	}
	return fmt.Sprintf("github %s: HTTP %d%s%s", e.Op, e.Status, msg, hint)
}

// IsStatus reports whether err is a StatusError with the given code.
func IsStatus(err error, code int) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Status == code
}

type Client struct {
	cfg     config.Config
	baseURL string
	http    *http.Client
	token   func() (string, error)
}

// NewClient builds a client for cfg's repo. baseURL "" uses cfg.APIBase().
func NewClient(cfg config.Config, baseURL string) *Client {
	if baseURL == "" {
		baseURL = cfg.APIBase()
	}
	return &Client{
		cfg:     cfg,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 60 * time.Second},
		token:   cfg.ResolveToken,
	}
}

func (c *Client) repoPath() string {
	return fmt.Sprintf("/repos/%s/%s", url.PathEscape(c.cfg.Owner), url.PathEscape(c.cfg.Repo))
}

func (c *Client) contentsURL(path string) string {
	segments := strings.Split(path, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return c.baseURL + c.repoPath() + "/contents/" + strings.Join(segments, "/")
}

func (c *Client) do(ctx context.Context, method, u string, body any, accept string) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return nil, err
	}
	tok, err := c.token()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if accept == "" {
		accept = "application/vnd.github+json"
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "team-memory")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

func statusErr(op string, resp *http.Response) error {
	var body struct {
		Message string `json:"message"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	_ = json.Unmarshal(data, &body)
	return &StatusError{Op: op, Status: resp.StatusCode, Message: body.Message}
}

// User is the authenticated GitHub user.
type User struct {
	Login string `json:"login"`
}

func (c *Client) GetUser(ctx context.Context) (User, error) {
	resp, err := c.do(ctx, http.MethodGet, c.baseURL+"/user", nil, "")
	if err != nil {
		return User{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return User{}, statusErr("GET /user", resp)
	}
	var u User
	return u, json.NewDecoder(resp.Body).Decode(&u)
}

// Repo describes the memory repository.
type Repo struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Permissions   struct {
		Push bool `json:"push"`
	} `json:"permissions"`
}

func (c *Client) GetRepo(ctx context.Context) (Repo, error) {
	resp, err := c.do(ctx, http.MethodGet, c.baseURL+c.repoPath(), nil, "")
	if err != nil {
		return Repo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Repo{}, statusErr("GET repo", resp)
	}
	var r Repo
	return r, json.NewDecoder(resp.Body).Decode(&r)
}

// HeadSHA returns the commit SHA of the default branch, or ErrEmptyRepo.
func (c *Client) HeadSHA(ctx context.Context) (string, error) {
	resp, err := c.do(ctx, http.MethodGet, c.baseURL+c.repoPath()+"/commits/HEAD", nil, "application/vnd.github.sha")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 409 {
		return "", ErrEmptyRepo
	}
	if resp.StatusCode != 200 {
		return "", statusErr("GET commits/HEAD", resp)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(b))
	if strings.HasPrefix(sha, "{") { // server ignored the sha media type
		var obj struct {
			SHA string `json:"sha"`
		}
		_ = json.Unmarshal(b, &obj)
		sha = obj.SHA
	}
	if sha == "" {
		return "", errors.New("github: empty HEAD sha")
	}
	return sha, nil
}

// Tarball streams a gzipped tarball of the repo at ref.
func (c *Client) Tarball(ctx context.Context, ref string) (io.ReadCloser, error) {
	resp, err := c.do(ctx, http.MethodGet, c.baseURL+c.repoPath()+"/tarball/"+url.PathEscape(ref), nil, "")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		return nil, statusErr("GET tarball", resp)
	}
	return resp.Body, nil
}

func (c *Client) GetFile(ctx context.Context, path string) (FileInfo, error) {
	resp, err := c.do(ctx, http.MethodGet, c.contentsURL(path), nil, "")
	if err != nil {
		return FileInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return FileInfo{Exists: false}, nil
	}
	if resp.StatusCode >= 400 {
		return FileInfo{}, statusErr("GET "+path, resp)
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

// PutFile creates (sha == "") or replaces (sha = current blob sha) a file.
// Returns ErrConflict if the file changed or, for creates, already exists.
func (c *Client) PutFile(ctx context.Context, path, content, sha, message string) error {
	if err := ValidatePath(path); err != nil {
		return err
	}
	body := map[string]any{
		"message": message,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
	}
	if sha != "" {
		body["sha"] = sha
	}
	resp, err := c.do(ctx, http.MethodPut, c.contentsURL(path), body, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 409:
		return ErrConflict
	case resp.StatusCode == 422 && sha == "":
		// "sha wasn't supplied" — file already exists.
		return ErrConflict
	case resp.StatusCode >= 400:
		return statusErr("PUT "+path, resp)
	}
	return nil
}

// Update rewrites path by applying fn to its current content, retrying on
// concurrent modification. fn receives "" and exists=false for new files.
func (c *Client) Update(ctx context.Context, path, message string, fn func(content string, exists bool) (string, error)) error {
	for attempt := 0; attempt < 4; attempt++ {
		f, err := c.GetFile(ctx, path)
		if err != nil {
			return err
		}
		next, err := fn(f.Content, f.Exists)
		if err != nil {
			return err
		}
		if f.Exists && next == f.Content {
			return nil
		}
		sha := ""
		if f.Exists {
			sha = f.SHA
		}
		err = c.PutFile(ctx, path, next, sha, message)
		if errors.Is(err, ErrConflict) {
			continue
		}
		return err
	}
	return fmt.Errorf("team-memory: %s kept changing; gave up after 4 attempts", path)
}
