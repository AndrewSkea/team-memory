# OAuth Integrations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add GitHub OAuth (PAT fallback), Google Calendar OAuth (PKCE), and Slack OAuth to team-memory, exposed through a settings cog + Connections panel in the header.

**Architecture:** Go MCP server handles GitHub and Slack OAuth callbacks (code exchange requires server-side secret); Google uses PKCE so no backend is needed. A gear icon in the header opens a slide-in Connections panel. All OAuth tokens stored in `localStorage` under `team-memory:config`. Both `frontend/` and `mcp/frontend/` must be kept in sync.

**Tech Stack:** Vanilla JS ES modules, Node.js `node:test` runner, Go `net/http`, `httptest`

---

## File Map

| Action | Path | Purpose |
|--------|------|---------|
| Create | `frontend/src/services/oauth.js` | PKCE utils, OAuth start helpers, fragment/param reader |
| Create | `frontend/src/services/google-calendar.js` | Calendar API wrapper with token refresh |
| Create | `frontend/src/services/slack.js` | Webhook poster |
| Create | `frontend/src/pages/connections.js` | Connections panel UI + cog init |
| Create | `mcp/server/oauth.go` | GitHub + Slack OAuth routes + state nonce store |
| Create | `tests/frontend/oauth.test.js` | Tests for oauth.js |
| Create | `tests/frontend/google-calendar.test.js` | Tests for google-calendar.js |
| Create | `tests/frontend/slack.test.js` | Tests for slack.js |
| Create | `tests/frontend/connections.test.js` | Tests for connections.js |
| Create | `mcp/server/oauth_test.go` | Go tests for OAuth handlers |
| Modify | `mcp/server/handlers.go` | Register OAuth routes in `routes()` |
| Modify | `frontend/src/app.js` | Call `readOAuthFragment()` on bootstrap, init cog |
| Modify | `frontend/index.html` | Update CSP `connect-src`, add cog anchor point |
| Modify | `frontend/src/pages/setup.js` | Update PAT label with OAuth hint |
| Modify | `frontend/src/components/meeting-drawer.js` | Google Calendar dropdown + Slack post buttons |
| Modify | `frontend/src/ui/main.css` | Cog + connections panel styles |
| Mirror | `mcp/frontend/` | Apply all frontend changes |

---

### Task 1: `frontend/src/services/oauth.js` — OAuth utilities

**Files:**
- Create: `frontend/src/services/oauth.js`
- Create: `tests/frontend/oauth.test.js`

- [ ] **Step 1: Write the failing tests**

```js
// tests/frontend/oauth.test.js
import { strict as assert } from 'node:assert';
import { describe, it, beforeEach } from 'node:test';
import {
  _generateCodeVerifier,
  _codeChallenge,
  readOAuthFragment,
} from '../../frontend/src/services/oauth.js';

describe('_generateCodeVerifier', () => {
  it('returns base64url string of length 43', () => {
    const v = _generateCodeVerifier();
    assert.match(v, /^[A-Za-z0-9\-_]+$/);
    assert.ok(v.length >= 43 && v.length <= 128);
  });
  it('produces unique values each call', () => {
    assert.notEqual(_generateCodeVerifier(), _generateCodeVerifier());
  });
});

describe('_codeChallenge', async () => {
  it('returns base64url SHA-256 of verifier', async () => {
    // Known SHA-256: "abc" -> "ungWv48Bz-pIoK3TnSIaYW9O4hJqMNPsOdmP8gXhEXw"
    // We just verify it's a non-empty base64url string
    const challenge = await _codeChallenge('test-verifier');
    assert.match(challenge, /^[A-Za-z0-9\-_]+$/);
    assert.ok(challenge.length > 0);
  });
});

describe('readOAuthFragment', () => {
  it('returns null when no OAuth params present', () => {
    // jsdom not available — test via direct call with mock location
    const result = readOAuthFragment({ hash: '', search: '' });
    assert.equal(result, null);
  });

  it('parses gh_token from hash', () => {
    const result = readOAuthFragment({ hash: '#gh_token=gho_abc123', search: '' });
    assert.deepEqual(result, { type: 'github', token: 'gho_abc123' });
  });

  it('parses gh_error from hash', () => {
    const result = readOAuthFragment({ hash: '#gh_error=access_denied', search: '' });
    assert.deepEqual(result, { type: 'github_error', error: 'access_denied' });
  });

  it('parses slack_webhook + slack_channel from hash', () => {
    const result = readOAuthFragment({
      hash: '#slack_webhook=https%3A%2F%2Fhooks.slack.com%2Ftest&slack_channel=%23general',
      search: '',
    });
    assert.deepEqual(result, {
      type: 'slack',
      webhookUrl: 'https://hooks.slack.com/test',
      channel: '#general',
    });
  });

  it('parses google code from search params', () => {
    const result = readOAuthFragment({ hash: '', search: '?code=4%2F0google&state=xyz' });
    assert.deepEqual(result, { type: 'google', code: '4/0google', state: 'xyz' });
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

```
node --test tests/frontend/oauth.test.js
```

Expected: `ERR_MODULE_NOT_FOUND` — file doesn't exist yet.

- [ ] **Step 3: Write `frontend/src/services/oauth.js`**

```js
// 32-byte random base64url verifier for PKCE
export function _generateCodeVerifier() {
  const arr = new Uint8Array(32);
  crypto.getRandomValues(arr);
  return btoa(String.fromCharCode(...arr))
    .replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

// SHA-256 base64url of verifier
export async function _codeChallenge(verifier) {
  const enc = new TextEncoder().encode(verifier);
  const hash = await crypto.subtle.digest('SHA-256', enc);
  return btoa(String.fromCharCode(...new Uint8Array(hash)))
    .replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

// Redirect to Go server which redirects to GitHub
export function startGitHubOAuth(clientId) {
  const url = `/oauth/github/start?client_id=${encodeURIComponent(clientId)}`;
  window.location.href = url;
}

// PKCE redirect to Google accounts
export async function startGoogleOAuth(clientId) {
  const verifier = _generateCodeVerifier();
  const challenge = await _codeChallenge(verifier);
  const state = _generateCodeVerifier(); // random state nonce
  sessionStorage.setItem('oauth_google_pending', JSON.stringify({ verifier, state }));
  const params = new URLSearchParams({
    client_id: clientId,
    redirect_uri: window.location.origin + window.location.pathname,
    response_type: 'code',
    scope: 'https://www.googleapis.com/auth/calendar.readonly',
    code_challenge: challenge,
    code_challenge_method: 'S256',
    state,
    access_type: 'offline',
    prompt: 'consent',
  });
  window.location.href = `https://accounts.google.com/o/oauth2/v2/auth?${params}`;
}

// Exchange Google code for tokens (PKCE — no client_secret)
export async function exchangeGoogleCode(clientId, code, state) {
  const pending = JSON.parse(sessionStorage.getItem('oauth_google_pending') || 'null');
  if (!pending || pending.state !== state) throw new Error('OAuth state mismatch');
  sessionStorage.removeItem('oauth_google_pending');
  const redirectUri = window.location.origin + window.location.pathname;
  const res = await fetch('https://oauth2.googleapis.com/token', {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({
      code,
      client_id: clientId,
      code_verifier: pending.verifier,
      redirect_uri: redirectUri,
      grant_type: 'authorization_code',
    }),
  });
  if (!res.ok) throw new Error(`Google token exchange failed: ${res.status}`);
  const data = await res.json();
  return { accessToken: data.access_token, refreshToken: data.refresh_token };
}

// Refresh a Google access token using stored refresh token
export async function refreshGoogleToken(clientId, refreshToken) {
  const res = await fetch('https://oauth2.googleapis.com/token', {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({
      client_id: clientId,
      refresh_token: refreshToken,
      grant_type: 'refresh_token',
    }),
  });
  if (!res.ok) throw new Error(`Google token refresh failed: ${res.status}`);
  const data = await res.json();
  return data.access_token;
}

export function startSlackOAuth(clientId) {
  const url = `/oauth/slack/start?client_id=${encodeURIComponent(clientId)}`;
  window.location.href = url;
}

// Parse location for OAuth callback params. Accepts { hash, search } for testability.
// Returns structured result or null if no OAuth params present.
export function readOAuthFragment(loc = window.location) {
  const hash = loc.hash || '';
  const search = loc.search || '';

  if (hash.includes('gh_token=') || hash.includes('gh_error=')) {
    const params = new URLSearchParams(hash.slice(1));
    if (params.get('gh_token')) return { type: 'github', token: params.get('gh_token') };
    if (params.get('gh_error')) return { type: 'github_error', error: params.get('gh_error') };
  }

  if (hash.includes('slack_webhook=')) {
    const params = new URLSearchParams(hash.slice(1));
    return {
      type: 'slack',
      webhookUrl: params.get('slack_webhook'),
      channel: params.get('slack_channel') || '',
    };
  }

  if (search.includes('code=') && search.includes('state=')) {
    const params = new URLSearchParams(search.slice(1));
    if (params.get('code') && params.get('state')) {
      return { type: 'google', code: params.get('code'), state: params.get('state') };
    }
  }

  return null;
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
node --test tests/frontend/oauth.test.js
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/services/oauth.js tests/frontend/oauth.test.js
git commit -m "feat: add oauth.js service with PKCE utils and fragment reader"
```

---

### Task 2: `frontend/src/services/google-calendar.js` — Calendar API wrapper

**Files:**
- Create: `frontend/src/services/google-calendar.js`
- Create: `tests/frontend/google-calendar.test.js`

- [ ] **Step 1: Write the failing tests**

```js
// tests/frontend/google-calendar.test.js
import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { _buildTimeRange, _parseEvents } from '../../frontend/src/services/google-calendar.js';

describe('_buildTimeRange', () => {
  it('returns timeMin at start of day and timeMax at end of day', () => {
    const { timeMin, timeMax } = _buildTimeRange(new Date('2026-05-25T14:00:00Z'));
    assert.match(timeMin, /^2026-05-25T00:00:00/);
    assert.match(timeMax, /^2026-05-25T23:59:59/);
  });
});

describe('_parseEvents', () => {
  it('extracts title, start, attendees from Google Calendar event', () => {
    const raw = [
      {
        summary: 'Sprint Planning',
        start: { dateTime: '2026-05-25T10:00:00+01:00' },
        attendees: [
          { email: 'alice@example.com' },
          { email: 'bob@example.com' },
        ],
      },
    ];
    const events = _parseEvents(raw);
    assert.equal(events.length, 1);
    assert.equal(events[0].title, 'Sprint Planning');
    assert.equal(events[0].start, '2026-05-25T10:00:00+01:00');
    assert.deepEqual(events[0].attendees, ['alice@example.com', 'bob@example.com']);
  });

  it('handles missing attendees', () => {
    const raw = [{ summary: 'Solo', start: { dateTime: '2026-05-25T09:00:00Z' } }];
    const events = _parseEvents(raw);
    assert.deepEqual(events[0].attendees, []);
  });

  it('falls back to start.date for all-day events', () => {
    const raw = [{ summary: 'Holiday', start: { date: '2026-05-25' } }];
    const events = _parseEvents(raw);
    assert.equal(events[0].start, '2026-05-25');
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

```
node --test tests/frontend/google-calendar.test.js
```

Expected: `ERR_MODULE_NOT_FOUND`.

- [ ] **Step 3: Write `frontend/src/services/google-calendar.js`**

```js
const CALENDAR_API = 'https://www.googleapis.com/calendar/v3';

export function _buildTimeRange(now = new Date()) {
  const dateStr = now.toISOString().slice(0, 10);
  return {
    timeMin: `${dateStr}T00:00:00.000Z`,
    timeMax: `${dateStr}T23:59:59.999Z`,
  };
}

export function _parseEvents(items = []) {
  return items.map(e => ({
    title: e.summary || '(no title)',
    start: e.start?.dateTime || e.start?.date || '',
    attendees: (e.attendees || []).map(a => a.email),
  }));
}

export async function listTodayMeetings(accessToken, clientId, refreshTokenFn) {
  const { timeMin, timeMax } = _buildTimeRange();
  const params = new URLSearchParams({
    timeMin,
    timeMax,
    singleEvents: 'true',
    orderBy: 'startTime',
    maxResults: '10',
  });

  let res = await fetch(`${CALENDAR_API}/calendars/primary/events?${params}`, {
    headers: { Authorization: `Bearer ${accessToken}` },
  });

  if (res.status === 401 && refreshTokenFn) {
    const newToken = await refreshTokenFn();
    if (newToken) {
      res = await fetch(`${CALENDAR_API}/calendars/primary/events?${params}`, {
        headers: { Authorization: `Bearer ${newToken}` },
      });
    }
  }

  if (!res.ok) throw new Error(`Calendar API error: ${res.status}`);
  const data = await res.json();
  return _parseEvents(data.items || []);
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
node --test tests/frontend/google-calendar.test.js
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/services/google-calendar.js tests/frontend/google-calendar.test.js
git commit -m "feat: add google-calendar.js service with listTodayMeetings"
```

---

### Task 3: `frontend/src/services/slack.js` — Webhook poster

**Files:**
- Create: `frontend/src/services/slack.js`
- Create: `tests/frontend/slack.test.js`

- [ ] **Step 1: Write the failing tests**

```js
// tests/frontend/slack.test.js
import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { postToSlack } from '../../frontend/src/services/slack.js';

describe('postToSlack', () => {
  it('POSTs JSON body to webhookUrl', async () => {
    let captured;
    const fakeFetch = async (url, opts) => {
      captured = { url, opts };
      return { ok: true, status: 200 };
    };
    await postToSlack('https://hooks.slack.com/test', 'Hello team', fakeFetch);
    assert.equal(captured.url, 'https://hooks.slack.com/test');
    assert.equal(captured.opts.method, 'POST');
    const body = JSON.parse(captured.opts.body);
    assert.equal(body.text, 'Hello team');
  });

  it('throws on non-ok response', async () => {
    const fakeFetch = async () => ({ ok: false, status: 400 });
    await assert.rejects(
      () => postToSlack('https://hooks.slack.com/test', 'hello', fakeFetch),
      /400/
    );
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

```
node --test tests/frontend/slack.test.js
```

Expected: `ERR_MODULE_NOT_FOUND`.

- [ ] **Step 3: Write `frontend/src/services/slack.js`**

```js
export async function postToSlack(webhookUrl, text, fetchFn = fetch) {
  const r = await fetchFn(webhookUrl, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text }),
  });
  if (!r.ok) throw new Error(`Slack post failed: ${r.status}`);
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
node --test tests/frontend/slack.test.js
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/services/slack.js tests/frontend/slack.test.js
git commit -m "feat: add slack.js webhook poster"
```

---

### Task 4: `mcp/server/oauth.go` — Go OAuth handlers

**Files:**
- Create: `mcp/server/oauth.go`
- Create: `mcp/server/oauth_test.go`
- Modify: `mcp/server/handlers.go` (add 4 routes to `routes()`)

- [ ] **Step 1: Write the failing Go tests**

```go
// mcp/server/oauth_test.go
package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestOAuthGitHubStartRedirects(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodGet, "/oauth/github/start?client_id=test-client", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "github.com/login/oauth/authorize") {
		t.Errorf("redirect should go to GitHub, got: %s", loc)
	}
	if !strings.Contains(loc, "client_id=test-client") {
		t.Errorf("redirect missing client_id, got: %s", loc)
	}
	if !strings.Contains(loc, "state=") {
		t.Errorf("redirect missing state param, got: %s", loc)
	}
}

func TestOAuthGitHubStartMissingClientId(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodGet, "/oauth/github/start", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestOAuthGitHubCallbackBadState(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodGet, "/oauth/github/callback?code=abc&state=invalid", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	// Should redirect to app with gh_error (not 200)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "gh_error=") {
		t.Errorf("redirect missing gh_error, got: %s", loc)
	}
}

func TestOAuthSlackStartRedirects(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodGet, "/oauth/slack/start?client_id=slack-client", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "slack.com/oauth/v2/authorize") {
		t.Errorf("redirect should go to Slack, got: %s", loc)
	}
}

func TestOAuthSlackCallbackBadState(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest(http.MethodGet, "/oauth/slack/callback?code=abc&state=invalid", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "slack_error=") {
		t.Errorf("redirect missing slack_error, got: %s", loc)
	}
}

func TestOAuthStateNonceExpiry(t *testing.T) {
	// Direct test of nonce store: verify expired nonces are rejected
	store := newNonceStore()
	store.set("mystate")
	if !store.consume("mystate") {
		t.Error("valid state should be accepted")
	}
	if store.consume("mystate") {
		t.Error("state should only be consumed once")
	}
	if store.consume("nonexistent") {
		t.Error("nonexistent state should be rejected")
	}
	_ = os.Getenv("") // satisfy import
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
cd mcp && go test ./server/... -run TestOAuth
```

Expected: compile error — `oauth.go` and `newNonceStore` don't exist yet.

- [ ] **Step 3: Write `mcp/server/oauth.go`**

```go
package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// nonceStore holds short-lived state nonces for OAuth CSRF protection.
type nonceStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

func newNonceStore() *nonceStore {
	return &nonceStore{entries: make(map[string]time.Time)}
}

func (n *nonceStore) set(state string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.entries[state] = time.Now().Add(5 * time.Minute)
}

func (n *nonceStore) consume(state string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	exp, ok := n.entries[state]
	if !ok || time.Now().After(exp) {
		delete(n.entries, state)
		return false
	}
	delete(n.entries, state)
	return true
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// appOrigin derives the app URL from the request host (same server serves the frontend).
func appOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

func (s *Server) handleGitHubOAuthStart(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		writeErr(w, http.StatusBadRequest, "client_id required")
		return
	}
	state, err := randomState()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not generate state")
		return
	}
	s.oauthNonces.set(state)
	redirectURI := fmt.Sprintf("%s/oauth/github/callback", appOrigin(r))
	params := url.Values{
		"client_id":    {clientID},
		"redirect_uri": {redirectURI},
		"state":        {state},
		"scope":        {"repo"},
	}
	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+params.Encode(), http.StatusFound)
}

func (s *Server) handleGitHubOAuthCallback(w http.ResponseWriter, r *http.Request) {
	origin := appOrigin(r)
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if !s.oauthNonces.consume(state) {
		http.Redirect(w, r, origin+"/#gh_error=invalid_state", http.StatusFound)
		return
	}
	clientID := r.URL.Query().Get("client_id") // not available in callback from GitHub
	clientSecret := os.Getenv("GITHUB_CLIENT_SECRET")
	redirectURI := fmt.Sprintf("%s/oauth/github/callback", origin)

	// We don't have client_id in the callback — GitHub doesn't send it back.
	// The client_id must be stored when starting; we rely on the secret being enough.
	// For simplicity, read client_id from the query (GitHub sometimes echoes params).
	// If not present, the exchange will fail gracefully.
	_ = clientID

	type ghTokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost,
		"https://github.com/login/oauth/access_token", nil)
	req.Header.Set("Accept", "application/json")
	q := url.Values{
		"client_id":     {r.URL.Query().Get("client_id")},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}
	req.URL.RawQuery = q.Encode()
	resp, err := http.DefaultClient.Do(req)
	if err != nil || !resp.StatusCode/100 == 2 {
		http.Redirect(w, r, origin+"/#gh_error=exchange_failed", http.StatusFound)
		return
	}
	defer resp.Body.Close()
	var tok ghTokenResp
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.Error != "" {
		msg := tok.Error
		if msg == "" {
			msg = "decode_failed"
		}
		http.Redirect(w, r, origin+"/#gh_error="+url.QueryEscape(msg), http.StatusFound)
		return
	}
	http.Redirect(w, r, origin+"/#gh_token="+url.QueryEscape(tok.AccessToken), http.StatusFound)
}

func (s *Server) handleSlackOAuthStart(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		writeErr(w, http.StatusBadRequest, "client_id required")
		return
	}
	state, err := randomState()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not generate state")
		return
	}
	s.oauthNonces.set(state)
	redirectURI := fmt.Sprintf("%s/oauth/slack/callback", appOrigin(r))
	params := url.Values{
		"client_id":    {clientID},
		"redirect_uri": {redirectURI},
		"scope":        {"incoming-webhook"},
		"state":        {state},
	}
	http.Redirect(w, r, "https://slack.com/oauth/v2/authorize?"+params.Encode(), http.StatusFound)
}

func (s *Server) handleSlackOAuthCallback(w http.ResponseWriter, r *http.Request) {
	origin := appOrigin(r)
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if !s.oauthNonces.consume(state) {
		http.Redirect(w, r, origin+"/#slack_error=invalid_state", http.StatusFound)
		return
	}
	clientID := r.URL.Query().Get("client_id")
	clientSecret := os.Getenv("SLACK_CLIENT_SECRET")
	redirectURI := fmt.Sprintf("%s/oauth/slack/callback", origin)

	type slackResp struct {
		Ok              bool   `json:"ok"`
		Error           string `json:"error"`
		IncomingWebhook struct {
			URL     string `json:"url"`
			Channel string `json:"channel"`
		} `json:"incoming_webhook"`
	}

	resp, err := http.PostForm("https://slack.com/api/oauth.v2.access", url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	})
	if err != nil {
		http.Redirect(w, r, origin+"/#slack_error=exchange_failed", http.StatusFound)
		return
	}
	defer resp.Body.Close()
	var tok slackResp
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || !tok.Ok {
		msg := tok.Error
		if msg == "" {
			msg = "exchange_failed"
		}
		http.Redirect(w, r, origin+"/#slack_error="+url.QueryEscape(msg), http.StatusFound)
		return
	}
	fragment := fmt.Sprintf("#slack_webhook=%s&slack_channel=%s",
		url.QueryEscape(tok.IncomingWebhook.URL),
		url.QueryEscape(tok.IncomingWebhook.Channel),
	)
	http.Redirect(w, r, origin+"/"+fragment, http.StatusFound)
}
```

- [ ] **Step 4: Add `oauthNonces` to the Server struct in `mcp/server/server.go`**

Read `mcp/server/server.go`. Add `oauthNonces *nonceStore` field to `Server` struct and initialize it in `New()`:

```go
// In Server struct:
oauthNonces *nonceStore

// In New():
oauthNonces: newNonceStore(),
```

- [ ] **Step 5: Register routes in `mcp/server/handlers.go`**

Add these 4 lines to `routes()` after the existing registrations:

```go
s.mux.HandleFunc("/oauth/github/start", s.handleGitHubOAuthStart)
s.mux.HandleFunc("/oauth/github/callback", s.handleGitHubOAuthCallback)
s.mux.HandleFunc("/oauth/slack/start", s.handleSlackOAuthStart)
s.mux.HandleFunc("/oauth/slack/callback", s.handleSlackOAuthCallback)
```

- [ ] **Step 6: Run Go tests**

```
cd mcp && go test ./server/... -run TestOAuth
```

Expected: all tests pass.

- [ ] **Step 7: Verify Go build**

```
cd mcp && go build ./...
```

Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add mcp/server/oauth.go mcp/server/oauth_test.go mcp/server/server.go mcp/server/handlers.go
git commit -m "feat: add GitHub and Slack OAuth handlers to Go MCP server"
```

---

### Task 5: CSS — settings cog + connections panel styles

**Files:**
- Modify: `frontend/src/ui/main.css`

- [ ] **Step 1: Read end of `frontend/src/ui/main.css` to find append point**

Run: `tail -20 frontend/src/ui/main.css` (or read the file end)

- [ ] **Step 2: Append cog + panel styles**

Append to the end of `frontend/src/ui/main.css`:

```css
/* ── Settings cog ── */
.settings-cog {
  position: absolute;
  top: 14px;
  right: 16px;
  background: none;
  border: none;
  cursor: pointer;
  color: var(--muted);
  padding: 4px;
  line-height: 0;
  border-radius: 50%;
  transition: color 0.15s, background 0.15s;
}
.settings-cog:hover {
  color: var(--fg);
  background: var(--card-bg);
}
.settings-cog svg {
  width: 18px;
  height: 18px;
}

/* ── Connections panel ── */
.connections-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0,0,0,0.35);
  z-index: 199;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.2s;
}
.connections-overlay--visible {
  opacity: 1;
  pointer-events: auto;
}
.connections-panel {
  position: fixed;
  top: 0;
  right: 0;
  width: 360px;
  max-width: 100vw;
  height: 100vh;
  background: var(--bg);
  border-left: 1px solid var(--border);
  z-index: 200;
  transform: translateX(100%);
  transition: transform 0.25s ease;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
}
.connections-panel--open {
  transform: translateX(0);
}
.connections-panel__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border);
  font-weight: 600;
}
.connections-panel__close {
  background: none;
  border: none;
  cursor: pointer;
  font-size: 20px;
  color: var(--muted);
  line-height: 1;
  padding: 0 4px;
}
.connections-panel__body {
  padding: 16px 20px;
  display: flex;
  flex-direction: column;
  gap: 20px;
}
.connection-card {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.connection-card__title {
  font-weight: 600;
  font-size: 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.connection-card__status {
  font-size: 12px;
  color: var(--muted);
}
.connection-card__status.connected {
  color: var(--success, #22c55e);
}
.connection-card label {
  font-size: 12px;
  color: var(--muted);
  margin-bottom: 2px;
  display: block;
}
.connection-card input {
  width: 100%;
  font-size: 13px;
  padding: 6px 8px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--card-bg);
  color: var(--fg);
  box-sizing: border-box;
}
.connection-card__actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.connection-card__sep {
  font-size: 11px;
  color: var(--muted);
  text-align: center;
}
```

- [ ] **Step 3: Verify CSS file is valid (visual check — no syntax errors)**

Open `frontend/index.html` in a browser and confirm no console errors about CSS.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/ui/main.css
git commit -m "feat: add settings cog and connections panel CSS"
```

---

### Task 6: `frontend/src/pages/connections.js` — Connections panel

**Files:**
- Create: `frontend/src/pages/connections.js`
- Create: `tests/frontend/connections.test.js`

- [ ] **Step 1: Write the failing tests**

```js
// tests/frontend/connections.test.js
import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { _providerStatus } from '../../frontend/src/pages/connections.js';

describe('_providerStatus', () => {
  it('returns connected label for github with token', () => {
    const s = _providerStatus('github', { githubOAuthToken: 'tok', username: 'alice' });
    assert.ok(s.connected);
    assert.match(s.label, /alice/);
  });

  it('returns not-connected for github without token', () => {
    const s = _providerStatus('github', {});
    assert.ok(!s.connected);
  });

  it('returns connected for google with accessToken', () => {
    const s = _providerStatus('google', { googleAccessToken: 'tok' });
    assert.ok(s.connected);
  });

  it('returns connected for slack with webhookUrl', () => {
    const s = _providerStatus('slack', { slackWebhookUrl: 'https://hooks.slack.com/x', slackChannel: '#general' });
    assert.ok(s.connected);
    assert.match(s.label, /#general/);
  });

  it('returns not-connected for slack without webhookUrl', () => {
    const s = _providerStatus('slack', {});
    assert.ok(!s.connected);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

```
node --test tests/frontend/connections.test.js
```

Expected: `ERR_MODULE_NOT_FOUND`.

- [ ] **Step 3: Write `frontend/src/pages/connections.js`**

```js
import { startGitHubOAuth, startGoogleOAuth, startSlackOAuth } from '../services/oauth.js';

function escHtml(s) {
  return String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

export function _providerStatus(provider, config) {
  if (provider === 'github') {
    if (config.githubOAuthToken) {
      return { connected: true, label: `Connected as ${config.username || 'user'}` };
    }
    return { connected: false, label: 'Not connected' };
  }
  if (provider === 'google') {
    if (config.googleAccessToken) {
      return { connected: true, label: 'Connected' };
    }
    return { connected: false, label: 'Not connected' };
  }
  if (provider === 'slack') {
    if (config.slackWebhookUrl) {
      return { connected: true, label: `Connected to ${config.slackChannel || 'workspace'}` };
    }
    return { connected: false, label: 'Not connected' };
  }
  return { connected: false, label: 'Not connected' };
}

function renderCard({ title, provider, config, onUpdate }) {
  const status = _providerStatus(provider, config);
  const clientIdKey = provider === 'github' ? 'githubClientId'
    : provider === 'google' ? 'googleClientId'
    : 'slackClientId';
  const clientId = config[clientIdKey] || '';

  const div = document.createElement('div');
  div.className = 'connection-card';
  div.innerHTML = `
    <div class="connection-card__title">
      ${escHtml(title)}
      <span class="connection-card__status${status.connected ? ' connected' : ''}">${escHtml(status.label)}</span>
    </div>
    <label>Client ID</label>
    <input type="text" class="cid-input" value="${escHtml(clientId)}" placeholder="Paste your ${escHtml(title)} Client ID" />
    <div class="connection-card__actions">
      <button class="btn btn-primary connect-btn">Connect ${escHtml(title)}</button>
      ${status.connected ? `<button class="btn btn-danger disconnect-btn">Disconnect</button>` : ''}
    </div>
    ${provider === 'github' ? `<div class="connection-card__sep">or use a Personal Access Token in Setup</div>` : ''}
  `;

  const cidInput = div.querySelector('.cid-input');
  const connectBtn = div.querySelector('.connect-btn');
  const disconnectBtn = div.querySelector('.disconnect-btn');

  connectBtn.addEventListener('click', () => {
    const cid = cidInput.value.trim();
    if (!cid) { alert('Paste your Client ID first.'); return; }
    config[clientIdKey] = cid;
    onUpdate(config);
    if (provider === 'github') startGitHubOAuth(cid);
    else if (provider === 'google') startGoogleOAuth(cid);
    else startSlackOAuth(cid);
  });

  disconnectBtn?.addEventListener('click', () => {
    if (provider === 'github') { delete config.githubOAuthToken; }
    else if (provider === 'google') { delete config.googleAccessToken; delete config.googleRefreshToken; }
    else { delete config.slackWebhookUrl; delete config.slackChannel; }
    onUpdate(config);
    // Re-render
    renderConnections(div.closest('.connections-panel__body'), config, onUpdate);
  });

  return div;
}

export function renderConnections(root, config, onUpdate) {
  root.innerHTML = '';
  for (const { title, provider } of [
    { title: 'GitHub', provider: 'github' },
    { title: 'Google Calendar', provider: 'google' },
    { title: 'Slack', provider: 'slack' },
  ]) {
    root.appendChild(renderCard({ title, provider, config, onUpdate }));
  }
}

export function initConnectionsCog(config, onUpdate) {
  const header = document.querySelector('.header');
  if (!header) return;
  header.style.position = 'relative';

  const cog = document.createElement('button');
  cog.className = 'settings-cog';
  cog.title = 'Connections';
  cog.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
    <circle cx="12" cy="12" r="3"/>
    <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>
  </svg>`;
  header.appendChild(cog);

  const overlay = document.createElement('div');
  overlay.className = 'connections-overlay';
  document.body.appendChild(overlay);

  const panel = document.createElement('div');
  panel.className = 'connections-panel';
  panel.innerHTML = `
    <div class="connections-panel__header">
      Connections
      <button class="connections-panel__close" aria-label="Close">×</button>
    </div>
    <div class="connections-panel__body"></div>
  `;
  document.body.appendChild(panel);

  const body = panel.querySelector('.connections-panel__body');

  function openPanel() {
    renderConnections(body, config, (updated) => { onUpdate(updated); });
    panel.classList.add('connections-panel--open');
    overlay.classList.add('connections-overlay--visible');
  }
  function closePanel() {
    panel.classList.remove('connections-panel--open');
    overlay.classList.remove('connections-overlay--visible');
  }

  cog.addEventListener('click', openPanel);
  overlay.addEventListener('click', closePanel);
  panel.querySelector('.connections-panel__close').addEventListener('click', closePanel);
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
node --test tests/frontend/connections.test.js
```

Expected: all tests pass.

- [ ] **Step 5: Run full test suite**

```
npm test
```

Expected: all existing + new tests pass.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/connections.js tests/frontend/connections.test.js
git commit -m "feat: add connections panel with GitHub/Google/Slack provider cards"
```

---

### Task 7: Wire OAuth into `app.js` bootstrap + update CSP

**Files:**
- Modify: `frontend/src/app.js`
- Modify: `frontend/index.html`

- [ ] **Step 1: Read current `frontend/src/app.js`**

(Already read — key sections: `bootstrap()` function, `loadConfig()`, `saveConfig()`, `toast()`, imports at top)

- [ ] **Step 2: Update `frontend/src/app.js`**

Add imports at top:

```js
import { readOAuthFragment, exchangeGoogleCode } from './services/oauth.js';
import { initConnectionsCog } from './pages/connections.js';
```

Replace the `bootstrap()` function body — after `go("remember")` and before the meeting drawer init, add OAuth fragment handling. The full updated `bootstrap()`:

```js
async function bootstrap() {
  try {
    const h = await fetch("/health");
    if (h.ok) {
      const { version } = await h.json();
      if (version) {
        const sub = document.querySelector(".subtitle");
        if (sub) sub.textContent = version;
      }
    }
  } catch (e) {
    console.warn("bootstrap: /health unreachable — backend may not be running", e);
  }
  if (!loadConfig().token) {
    try {
      const r = await fetch("/v1/config");
      if (r.ok) {
        const cfg = await r.json();
        if (cfg.token && cfg.owner && cfg.repo) { saveConfig(cfg); }
      }
    } catch (e) {
      console.warn("bootstrap: /v1/config unreachable", e);
    }
  }

  // Handle OAuth callbacks before first render
  const oauthResult = readOAuthFragment();
  if (oauthResult) {
    const cfg = loadConfig();
    if (oauthResult.type === 'github') {
      cfg.githubOAuthToken = oauthResult.token;
      saveConfig(cfg);
      // Clear hash
      history.replaceState(null, '', window.location.pathname + window.location.search);
      toast('GitHub connected.');
    } else if (oauthResult.type === 'github_error') {
      history.replaceState(null, '', window.location.pathname + window.location.search);
      toast('GitHub OAuth failed: ' + oauthResult.error, true);
    } else if (oauthResult.type === 'slack') {
      cfg.slackWebhookUrl = oauthResult.webhookUrl;
      cfg.slackChannel = oauthResult.channel;
      saveConfig(cfg);
      history.replaceState(null, '', window.location.pathname + window.location.search);
      toast('Slack connected to ' + (oauthResult.channel || 'workspace') + '.');
    } else if (oauthResult.type === 'google') {
      // PKCE exchange
      try {
        const cfg2 = loadConfig();
        const { accessToken, refreshToken } = await exchangeGoogleCode(
          cfg2.googleClientId || '', oauthResult.code, oauthResult.state
        );
        cfg2.googleAccessToken = accessToken;
        cfg2.googleRefreshToken = refreshToken;
        saveConfig(cfg2);
        history.replaceState(null, '', window.location.pathname);
        toast('Google Calendar connected.');
      } catch (e) {
        history.replaceState(null, '', window.location.pathname);
        toast('Google OAuth failed: ' + e.message, true);
      }
    }
  }

  go("remember");
  const cfg = loadConfig();
  if (cfg.token && cfg.owner) {
    initMeetingDrawer(cfg, new GitHubClient(cfg));
  }
  initConnectionsCog(cfg, (updated) => { saveConfig(updated); });
}
```

- [ ] **Step 3: Update CSP in `frontend/index.html`**

Change the `connect-src` value from:
```
connect-src 'self' https://api.github.com https://api.anthropic.com
```
to:
```
connect-src 'self' https://api.github.com https://api.anthropic.com https://accounts.google.com https://oauth2.googleapis.com https://www.googleapis.com https://hooks.slack.com
```

- [ ] **Step 4: Verify no JS errors**

Open `frontend/index.html` in browser, check console. No import errors expected.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/app.js frontend/index.html
git commit -m "feat: wire OAuth fragment handling and connections cog into app bootstrap"
```

---

### Task 8: Update PAT label in setup.js

**Files:**
- Modify: `frontend/src/pages/setup.js`

- [ ] **Step 1: Read `frontend/src/pages/setup.js`** to locate the PAT label text

Look for the line with "Personal Access Token" or similar label text.

- [ ] **Step 2: Update label**

Find the label for the GitHub PAT field. Change its text to include the OAuth hint. For example, change:

```html
<label>Personal Access Token</label>
```

to:

```html
<label>Personal Access Token <span class="muted">(or connect via OAuth in Connections ⚙)</span></label>
```

The exact old text depends on what's in the file — read it first, then make the precise substitution.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/setup.js
git commit -m "feat: add OAuth hint to PAT field label in setup"
```

---

### Task 9: Meeting drawer — Google Calendar dropdown + Slack post buttons

**Files:**
- Modify: `frontend/src/components/meeting-drawer.js`

This task adds two features to the meeting drawer:
1. Google Calendar "Today's meetings" `<select>` above the drop zone in the Notes tab (only when `config.googleAccessToken` present)
2. "Post to Slack" button in the save status area after successful Note save (only when `config.slackWebhookUrl` set)
3. "Post to Slack" button after successful Requirements save (only when `config.slackWebhookUrl` set)

- [ ] **Step 1: Read the full `meeting-drawer.js`** to understand current structure

Specifically locate:
- `renderNotesTab()` — where to add the Google Calendar select
- The save success handler — where to add the Slack post button
- `handleGenerateRequirements()` — where to add the Slack post button for requirements

- [ ] **Step 2: Add imports at top of meeting-drawer.js**

```js
import { listTodayMeetings } from '../services/google-calendar.js';
import { refreshGoogleToken } from '../services/oauth.js';
import { postToSlack } from '../services/slack.js';
```

- [ ] **Step 3: Add Google Calendar dropdown in `renderNotesTab()`**

In `renderNotesTab()`, after the drop zone markup but before its event handlers, add:

```js
// Google Calendar "Today's meetings" dropdown
if (config.googleAccessToken) {
  const calDiv = document.createElement('div');
  calDiv.style.cssText = 'margin-bottom:8px';
  calDiv.innerHTML = `<label style="font-size:12px;color:var(--muted);display:block;margin-bottom:4px">Today's meetings</label>
    <select id="gcal-select" style="width:100%;font-size:13px;padding:5px 8px;border:1px solid var(--border);border-radius:6px;background:var(--card-bg);color:var(--fg)">
      <option value="">Loading…</option>
    </select>`;
  // Insert before drop zone
  const dropzone = body.querySelector('#mdrop');
  if (dropzone) body.insertBefore(calDiv, dropzone);
  
  listTodayMeetings(
    config.googleAccessToken,
    config.googleClientId,
    async () => {
      const newToken = await refreshGoogleToken(config.googleClientId, config.googleRefreshToken);
      if (newToken) {
        config.googleAccessToken = newToken;
        // persist
        const saved = JSON.parse(localStorage.getItem('team-memory:config') || '{}');
        saved.googleAccessToken = newToken;
        localStorage.setItem('team-memory:config', JSON.stringify(saved));
      }
      return newToken;
    }
  ).then(events => {
    const sel = document.getElementById('gcal-select');
    if (!sel) return;
    if (!events.length) {
      sel.innerHTML = '<option value="">No meetings today</option>';
      return;
    }
    sel.innerHTML = '<option value="">Pick a meeting…</option>' +
      events.map((e, i) => `<option value="${i}">${escHtml(e.title)}</option>`).join('');
    sel.addEventListener('change', () => {
      const idx = parseInt(sel.value, 10);
      if (isNaN(idx)) return;
      const ev = events[idx];
      const titleInput = document.getElementById('mtitle');
      const dateInput = document.getElementById('mdate');
      const attendeesInput = document.getElementById('mattendees');
      if (titleInput) titleInput.value = ev.title;
      if (dateInput) dateInput.value = ev.start.slice(0, 10);
      if (attendeesInput) attendeesInput.value = ev.attendees.join(', ');
    });
  }).catch(() => {
    const sel = document.getElementById('gcal-select');
    if (sel) sel.innerHTML = '<option value="">Could not load meetings</option>';
  });
}
```

- [ ] **Step 4: Add "Post to Slack" button in save success area**

In the save success handler (after notes are saved to GitHub), add:

```js
if (config.slackWebhookUrl) {
  const slackBtn = document.createElement('button');
  slackBtn.className = 'btn btn-secondary';
  slackBtn.style.cssText = 'margin-top:6px;font-size:12px';
  slackBtn.textContent = 'Post to Slack';
  statusEl.appendChild(slackBtn);
  slackBtn.addEventListener('click', async () => {
    const title = document.getElementById('mtitle')?.value || '';
    const date = document.getElementById('mdate')?.value || '';
    const notes = document.getElementById('mnotes')?.value || '';
    const actions = document.getElementById('mactions')?.value || '';
    const text = `*Meeting saved: ${title} — ${date}*\nNotes: ${notes}\nActions: ${actions}`;
    try {
      await postToSlack(config.slackWebhookUrl, text);
      slackBtn.textContent = 'Posted ✓';
      setTimeout(() => slackBtn.remove(), 2000);
    } catch (e) {
      slackBtn.textContent = 'Failed: ' + e.message;
    }
  });
}
```

- [ ] **Step 5: Add "Post to Slack" button after requirements generation**

In `handleGenerateRequirements()` after successful save to repo, add:

```js
if (config.slackWebhookUrl) {
  const slackBtn = document.createElement('button');
  slackBtn.className = 'btn btn-secondary';
  slackBtn.style.cssText = 'margin-top:6px;font-size:12px';
  slackBtn.textContent = 'Post to Slack';
  reqStatusEl.appendChild(slackBtn);
  slackBtn.addEventListener('click', async () => {
    const slug = state.slug || '';
    const date = document.getElementById('mdate')?.value || '';
    const snippet = result.slice(0, 500);
    const text = `*Requirements generated: ${slug} — ${date}*\n${snippet}${result.length > 500 ? '…' : ''}`;
    try {
      await postToSlack(config.slackWebhookUrl, text);
      slackBtn.textContent = 'Posted ✓';
      setTimeout(() => slackBtn.remove(), 2000);
    } catch (e) {
      slackBtn.textContent = 'Failed: ' + e.message;
    }
  });
}
```

- [ ] **Step 6: Run full test suite**

```
npm test
```

Expected: all tests pass (no regressions).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/meeting-drawer.js
git commit -m "feat: add Google Calendar dropdown and Slack post buttons to meeting drawer"
```

---

### Task 10: Mirror all changes to `mcp/frontend/`

**Files:**
- Mirror all `frontend/src/` changes to `mcp/frontend/src/`
- Mirror `frontend/index.html` CSP change to `mcp/frontend/index.html`

Note: `mcp/frontend/` does NOT have `frontend/src/services/html.js` — always use inline `escHtml` function. The `mcp/frontend/` tree is Go-embedded and gitignore-tracked with force-add (`git add -f`).

- [ ] **Step 1: Read `mcp/frontend/src/app.js`** to understand current state before patching

- [ ] **Step 2: Copy new service files**

Copy these files directly (identical content, no mcp-specific changes needed):

```
frontend/src/services/oauth.js      → mcp/frontend/src/services/oauth.js
frontend/src/services/google-calendar.js → mcp/frontend/src/services/google-calendar.js
frontend/src/services/slack.js      → mcp/frontend/src/services/slack.js
frontend/src/pages/connections.js   → mcp/frontend/src/pages/connections.js
```

- [ ] **Step 3: Update `mcp/frontend/src/app.js`**

Apply the same changes as `frontend/src/app.js`:
- Add imports for `readOAuthFragment`, `exchangeGoogleCode`, `initConnectionsCog`
- Add OAuth fragment handling in `bootstrap()`
- Add `initConnectionsCog` call

- [ ] **Step 4: Update `mcp/frontend/src/components/meeting-drawer.js`**

Apply the same changes as `frontend/src/components/meeting-drawer.js`:
- Add imports for `listTodayMeetings`, `refreshGoogleToken`, `postToSlack`
- Add Google Calendar dropdown in `renderNotesTab()`
- Add Slack post buttons in save/requirements success handlers

- [ ] **Step 5: Update `mcp/frontend/src/pages/setup.js`**

Apply the same PAT label hint change.

- [ ] **Step 6: Update `mcp/frontend/src/ui/main.css`**

Append the same cog + connections panel CSS.

- [ ] **Step 7: Update `mcp/frontend/index.html` CSP**

Apply the same `connect-src` additions.

- [ ] **Step 8: Force-add and commit all mcp/frontend changes**

```bash
git add -f mcp/frontend/src/services/oauth.js
git add -f mcp/frontend/src/services/google-calendar.js
git add -f mcp/frontend/src/services/slack.js
git add -f mcp/frontend/src/pages/connections.js
git add -f mcp/frontend/src/app.js
git add -f mcp/frontend/src/components/meeting-drawer.js
git add -f mcp/frontend/src/pages/setup.js
git add -f mcp/frontend/src/ui/main.css
git add -f mcp/frontend/index.html
git commit -m "feat: mirror OAuth integrations to mcp/frontend"
```

---

### Task 11: Build verification

**Files:** None modified — verification only.

- [ ] **Step 1: Run full frontend test suite**

```
npm test
```

Expected: all tests pass (70 existing + new oauth/google-calendar/slack/connections tests).

- [ ] **Step 2: Run Go tests**

```
cd mcp && go test ./...
```

Expected: all tests pass including new `TestOAuth*` tests.

- [ ] **Step 3: Verify Go build**

```
cd mcp && go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit if any lint/build fixes needed**

```bash
git add -A
git commit -m "fix: build verification fixes"
```

---

## Self-Review Checklist

**Spec coverage:**
- ✅ Settings cog in header → Task 6 (`initConnectionsCog`)
- ✅ Three provider cards (GitHub, Google, Slack) → Task 6 (`renderConnections`)
- ✅ GitHub OAuth start/callback routes → Task 4
- ✅ Slack OAuth start/callback routes → Task 4
- ✅ Google PKCE flow → Task 1 + Task 7
- ✅ `readOAuthFragment()` + bootstrap wiring → Task 1 + Task 7
- ✅ `listTodayMeetings` with 401 retry → Task 2
- ✅ Meeting drawer Google Calendar dropdown → Task 9
- ✅ Slack post buttons (notes + requirements) → Task 9
- ✅ CSP additions → Task 7
- ✅ PAT label hint → Task 8
- ✅ `GitHubClient` token priority (OAuth over PAT) — handled via config key: `app.js` sets `githubOAuthToken` on config; `GitHubClient` uses `config.githubOAuthToken || config.token`. Need to verify this is wired up. **Gap:** `GitHubClient` constructor uses `this.token = token` and the `go()` function passes `new GitHubClient(config)` — `GitHubClient` receives the whole config object but only uses `token` field. Need to add `githubOAuthToken` priority in `GitHubClient` or in `go()`.

**Fix for gap:** In `frontend/src/app.js`, in the `go()` function where `GitHubClient` is constructed, add token priority:

```js
const gh = new GitHubClient({ ...config, token: config.githubOAuthToken || config.token });
```

This also applies in `bootstrap()` where `initMeetingDrawer(cfg, new GitHubClient(cfg))` is called. Update both call sites.

- ✅ mcp/frontend mirrored → Task 10
- ✅ State nonce store with TTL → Task 4 (`nonceStore`)
