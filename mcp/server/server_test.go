package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/AndrewSkea/team-memory/mcp/internal/testenv"
)

type client struct {
	t   *testing.T
	srv *httptest.Server
}

func (c client) do(method, path, body string, headers ...string) (int, map[string]any) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.srv.URL+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i] == "Host" {
			req.Host = headers[i+1]
			continue
		}
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = map[string]any{"_raw": string(b)}
	}
	return resp.StatusCode, out
}

func newClient(t *testing.T, configured bool) (client, *testenv.Env) {
	env := testenv.New(t, nil, configured)
	fe := fstest.MapFS{"index.html": {Data: []byte("<title>Team Memory</title>")}}
	srv := httptest.NewServer(New(env.App, "9.9.9", fe).Handler())
	t.Cleanup(srv.Close)
	return client{t, srv}, env
}

func TestHealthAndSetupFlow(t *testing.T) {
	c, env := newClient(t, false)
	code, body := c.do("GET", "/health", "")
	if code != 200 || body["version"] != "9.9.9" || body["configured"] != false {
		t.Fatalf("%d %v", code, body)
	}
	code, _ = c.do("GET", "/v1/memories?q=x", "")
	if code != 503 {
		t.Fatalf("unconfigured search: %d", code)
	}
	code, body = c.do("POST", "/v1/setup", `{"token":"bad","repo":"acme/kb"}`)
	if code != 400 || !strings.Contains(body["error"].(string), "invalid or expired") {
		t.Fatalf("bad token: %d %v", code, body)
	}
	code, body = c.do("POST", "/v1/setup", `{"token":"tok","repo":"acme/kb","mode":"review"}`)
	if code != 200 || body["username"] != "alice" {
		t.Fatalf("setup: %d %v", code, body)
	}
	code, body = c.do("GET", "/v1/settings", "")
	if code != 200 || body["effective_mode"] != "review" || body["token_source"] != "config" {
		t.Fatalf("settings: %v", body)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "tok\"") || strings.Contains(string(raw), `"token"`) {
		t.Fatalf("token leaked: %s", raw)
	}
	// Re-running setup keeping the stored token works (Settings page save).
	code, _ = c.do("POST", "/v1/setup", `{"keep_token":true,"repo":"acme/kb"}`)
	if code != 200 {
		t.Fatalf("keep token: %d", code)
	}
	_ = env
}

func TestMemoryLifecycle(t *testing.T) {
	c, _ := newClient(t, true)
	code, body := c.do("POST", "/v1/memories", `{"text":"Kafka consumer lag over 10k pages on-call","about":["kafka"],"type":"fact"}`)
	if code != 200 {
		t.Fatalf("create: %d %v", code, body)
	}
	id := body["memory"].(map[string]any)["id"].(string)
	code, body = c.do("GET", "/v1/memories?q=kafka+lag", "")
	if code != 200 || len(body["results"].([]any)) != 1 {
		t.Fatalf("search: %v", body)
	}
	code, body = c.do("GET", "/v1/memories", "")
	if code != 200 || len(body["results"].([]any)) != 1 {
		t.Fatalf("browse: %v", body)
	}
	code, body = c.do("GET", "/v1/memories/"+id, "")
	if code != 200 || body["title"] == "" {
		t.Fatalf("get: %v", body)
	}
	code, _ = c.do("GET", "/v1/memories/nope", "")
	if code != 404 {
		t.Fatalf("missing: %d", code)
	}
	code, body = c.do("POST", "/v1/memories/"+id+"/supersede", `{"text":"Lag threshold is now 50k"}`)
	if code != 200 {
		t.Fatalf("supersede: %v", body)
	}
	newID := body["memory"].(map[string]any)["id"].(string)
	code, _ = c.do("POST", "/v1/memories/"+newID+"/forget", `{"reason":"wrong"}`)
	if code != 200 {
		t.Fatalf("forget: %d", code)
	}
	code, body = c.do("GET", "/v1/stats", "")
	if code != 200 || body["active"].(float64) != 0 || body["total"].(float64) != 2 {
		t.Fatalf("stats: %v", body)
	}
	code, body = c.do("GET", "/v1/entities", "")
	if code != 200 || len(body["entities"].([]any)) != 1 {
		t.Fatalf("entities: %v", body)
	}
	code, body = c.do("POST", "/v1/quick-add", `{"text":"Deploys need migrations first."}`)
	if code != 200 || !strings.HasPrefix(body["file"].(string), "memories/") {
		t.Fatalf("quick-add: %v", body)
	}
	code, body = c.do("GET", "/v1/pending", "")
	if code != 200 || len(body["items"].([]any)) != 0 {
		t.Fatalf("pending: %v", body)
	}
	code, _ = c.do("POST", "/v1/pending/nope/approve", `{}`)
	if code != 404 {
		t.Fatalf("approve missing: %d", code)
	}
	code, body = c.do("PUT", "/v1/settings", `{"mode":"manual"}`)
	if code != 200 || body["settings"].(map[string]any)["effective_mode"] != "manual" {
		t.Fatalf("put settings: %v", body)
	}
	code, _ = c.do("POST", "/v1/sync", `{}`)
	if code != 200 {
		t.Fatalf("sync: %d", code)
	}
}

func TestSecurity(t *testing.T) {
	c, _ := newClient(t, true)
	// Cross-origin browser requests are refused.
	code, _ := c.do("POST", "/v1/memories", `{"text":"x"}`, "Origin", "http://evil.example")
	if code != 403 {
		t.Fatalf("cross-origin: %d", code)
	}
	// Form posts (no preflight) are refused.
	req, _ := http.NewRequest("POST", c.srv.URL+"/v1/memories", strings.NewReader("text=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 415 {
		t.Fatalf("form post: %d", resp.StatusCode)
	}
	// DNS rebinding: non-loopback Host refused.
	code, _ = c.do("GET", "/v1/settings", "", "Host", "evil.example")
	if code != 403 {
		t.Fatalf("rebinding: %d", code)
	}
	resp, _ = http.Get(c.srv.URL + "/")
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "connect-src 'self'") || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("headers: %v", resp.Header)
	}
	code, _ = c.do("GET", "/nope.xyz", "")
	if code != 404 {
		t.Fatalf("404: %d", code)
	}
}

func TestEmptyListsAreArrays(t *testing.T) {
	c, _ := newClient(t, true)
	for _, path := range []string{"/v1/memories?q=nothing+matches", "/v1/memories", "/v1/pending", "/v1/entities"} {
		_, body := c.do("GET", path, "")
		for k, v := range body {
			if v == nil {
				t.Errorf("%s: %q is null, want []", path, k)
			}
		}
	}
	_, body := c.do("GET", "/v1/stats", "")
	if body["top_entities"] == nil || body["contributors"] == nil {
		t.Errorf("stats has nulls: %v", body)
	}
}
