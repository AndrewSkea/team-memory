// Package fakegh is an in-memory fake of the subset of the GitHub REST API
// team-memory uses. It is used by unit/integration tests and, via
// cmd/fakegh, by the browser end-to-end tests.
package fakegh

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type Server struct {
	Owner, Repo, Login, Token string

	mu      sync.Mutex
	files   map[string]string
	commits int
	head    string
	// Puts records every successful write path, in order.
	Puts []string
	// FailPuts makes the next N PUTs return 500 (to test retries/offline).
	FailPuts int
}

func New(owner, repo, token string) *Server {
	return &Server{Owner: owner, Repo: repo, Login: owner, Token: token, files: map[string]string{}}
}

// Seed writes files without going through the API.
func (s *Server) Seed(files map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range files {
		s.files[k] = v
	}
	s.bump()
}

// File returns a file's content.
func (s *Server) File(path string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.files[path]
	return c, ok
}

// Paths lists all file paths.
func (s *Server) Paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Server) bump() {
	s.commits++
	h := sha1.Sum([]byte(fmt.Sprintf("commit-%d-%d", s.commits, time.Now().UnixNano())))
	s.head = hex.EncodeToString(h[:])
}

func blobSHA(content string) string {
	h := sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(content), content)))
	return hex.EncodeToString(h[:])
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.Token != "" && r.Header.Get("Authorization") != "Bearer "+s.Token {
		writeJSON(w, 401, map[string]string{"message": "Bad credentials"})
		return
	}
	p := r.URL.EscapedPath()
	if p == "/user" {
		writeJSON(w, 200, map[string]string{"login": s.Login})
		return
	}
	prefix := "/repos/" + s.Owner + "/" + s.Repo
	if !strings.HasPrefix(p, prefix) {
		writeJSON(w, 404, map[string]string{"message": "Not Found"})
		return
	}
	rest := strings.TrimPrefix(p, prefix)
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case rest == "" || rest == "/":
		writeJSON(w, 200, map[string]any{
			"full_name": s.Owner + "/" + s.Repo, "default_branch": "main", "private": true,
			"permissions": map[string]bool{"push": true},
		})
	case rest == "/commits/HEAD":
		if s.head == "" {
			writeJSON(w, 409, map[string]string{"message": "Git Repository is empty."})
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "sha") {
			_, _ = io.WriteString(w, s.head)
			return
		}
		writeJSON(w, 200, map[string]string{"sha": s.head})
	case strings.HasPrefix(rest, "/tarball/"):
		if s.head == "" {
			writeJSON(w, 404, map[string]string{"message": "Not Found"})
			return
		}
		w.Header().Set("Content-Type", "application/x-gzip")
		_ = s.writeTarball(w)
	case strings.HasPrefix(rest, "/contents/"):
		path, _ := url.PathUnescape(strings.TrimPrefix(rest, "/contents/"))
		s.contents(w, r, path)
	default:
		writeJSON(w, 404, map[string]string{"message": "Not Found"})
	}
}

func (s *Server) contents(w http.ResponseWriter, r *http.Request, path string) {
	switch r.Method {
	case http.MethodGet:
		c, ok := s.files[path]
		if !ok {
			writeJSON(w, 404, map[string]string{"message": "Not Found"})
			return
		}
		enc := base64.StdEncoding.EncodeToString([]byte(c))
		// GitHub wraps base64 at 60 chars.
		var wrapped strings.Builder
		for i := 0; i < len(enc); i += 60 {
			end := i + 60
			if end > len(enc) {
				end = len(enc)
			}
			wrapped.WriteString(enc[i:end] + "\n")
		}
		writeJSON(w, 200, map[string]string{"sha": blobSHA(c), "content": wrapped.String(), "encoding": "base64"})
	case http.MethodPut:
		if s.FailPuts > 0 {
			s.FailPuts--
			writeJSON(w, 500, map[string]string{"message": "boom"})
			return
		}
		var body struct {
			Message string `json:"message"`
			Content string `json:"content"`
			SHA     string `json:"sha"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"message": "bad json"})
			return
		}
		cur, exists := s.files[path]
		if exists && body.SHA == "" {
			writeJSON(w, 422, map[string]string{"message": "Invalid request.\n\n\"sha\" wasn't supplied."})
			return
		}
		if exists && body.SHA != blobSHA(cur) {
			writeJSON(w, 409, map[string]string{"message": "does not match"})
			return
		}
		if !exists && body.SHA != "" {
			writeJSON(w, 404, map[string]string{"message": "Not Found"})
			return
		}
		data, err := base64.StdEncoding.DecodeString(body.Content)
		if err != nil {
			writeJSON(w, 400, map[string]string{"message": "bad base64"})
			return
		}
		s.files[path] = string(data)
		s.Puts = append(s.Puts, path)
		s.bump()
		code := 200
		if !exists {
			code = 201
		}
		writeJSON(w, code, map[string]any{"content": map[string]string{"sha": blobSHA(string(data))}})
	default:
		writeJSON(w, 405, map[string]string{"message": "method"})
	}
}

func (s *Server) writeTarball(w io.Writer) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	root := fmt.Sprintf("%s-%s-%s/", s.Owner, s.Repo, s.head[:7])
	_ = tw.WriteHeader(&tar.Header{Name: root, Typeflag: tar.TypeDir, Mode: 0o755})
	paths := make([]string, 0, len(s.files))
	for k := range s.files {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	for _, p := range paths {
		c := s.files[p]
		if err := tw.WriteHeader(&tar.Header{Name: root + p, Mode: 0o644, Size: int64(len(c)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := io.Copy(tw, bytes.NewReader([]byte(c))); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}
