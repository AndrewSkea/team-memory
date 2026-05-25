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
type nonceEntry struct {
	exp      time.Time
	clientID string
}

type nonceStore struct {
	mu      sync.Mutex
	entries map[string]nonceEntry
}

func newNonceStore() *nonceStore {
	return &nonceStore{entries: make(map[string]nonceEntry)}
}

func (n *nonceStore) set(state, clientID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.entries[state] = nonceEntry{exp: time.Now().Add(5 * time.Minute), clientID: clientID}
}

// consume returns the stored clientID and true if the state is valid and not expired.
// Returns ("", false) if invalid, expired, or not found.
func (n *nonceStore) consume(state string) (clientID string, ok bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	e, exists := n.entries[state]
	delete(n.entries, state)
	if !exists || time.Now().After(e.exp) {
		return "", false
	}
	return e.clientID, true
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
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}
	return fmt.Sprintf("%s://%s", scheme, host)
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
	s.oauthNonces.set(state, clientID)
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
	clientID, ok := s.oauthNonces.consume(state)
	if !ok {
		http.Redirect(w, r, origin+"/#gh_error=invalid_state", http.StatusFound)
		return
	}
	clientSecret := os.Getenv("GITHUB_CLIENT_SECRET")
	redirectURI := fmt.Sprintf("%s/oauth/github/callback", origin)

	type ghTokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		"https://github.com/login/oauth/access_token", nil)
	if err != nil {
		http.Redirect(w, r, origin+"/#gh_error=internal_error", http.StatusFound)
		return
	}
	req.Header.Set("Accept", "application/json")
	q := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}
	req.URL.RawQuery = q.Encode()
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode/100 != 2 {
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
	s.oauthNonces.set(state, clientID)
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
	clientID, ok := s.oauthNonces.consume(state)
	if !ok {
		http.Redirect(w, r, origin+"/#slack_error=invalid_state", http.StatusFound)
		return
	}
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
