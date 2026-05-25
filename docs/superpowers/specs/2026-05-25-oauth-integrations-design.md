# OAuth Integrations — Design Spec
**Date:** 2026-05-25

## Overview

Add GitHub OAuth (with PAT fallback), Google Calendar OAuth, and Slack OAuth to team-memory. A settings cog in the header opens a Connections panel where users configure each integration. GitHub and Slack use the Go MCP server as OAuth callback handler; Google uses PKCE (pure browser).

---

## 1. Settings Cog + Connections Panel

### Entry point
A gear icon (`⚙`) fixed in the top-right of the `.header` div. Always visible. Click → right-side panel slides in (same pattern as the meeting drawer: overlay + `translateX` transition).

### Panel layout
Three connection cards, one per provider:

**GitHub**
- Status: "Connected as `login`" / "Not connected"
- Client ID input (stored in localStorage)
- "Connect via OAuth" button → triggers GitHub OAuth flow
- Separator: "or use a PAT" → existing PAT field (still works as fallback)
- Disconnect button (clears `githubOAuthToken`)

**Google Calendar**
- Status: "Connected as `email`" / "Not connected"
- Client ID input
- "Connect Google" button → triggers PKCE flow
- Disconnect button (clears `googleAccessToken`, `googleRefreshToken`)

**Slack**
- Status: "Connected to #channel-name" / "Not connected"
- Client ID input
- "Connect Slack" button → triggers Slack OAuth flow
- Disconnect button (clears `slackWebhookUrl`, `slackChannel`)

### New files
- `frontend/src/pages/connections.js` — `renderConnections(root, config, onUpdate)` + `initConnectionsCog(config)`
- `frontend/src/ui/main.css` — cog icon styles + connections panel styles (reuse drawer CSS patterns)

---

## 2. OAuth Architecture

### Token storage (additions to `team-memory:config` in localStorage)
| Key | Set by | Used by |
|-----|--------|---------|
| `githubClientId` | user input | frontend OAuth start |
| `githubOAuthToken` | GitHub callback | `GitHubClient` (overrides `token` PAT) |
| `googleClientId` | user input | PKCE flow |
| `googleAccessToken` | Google PKCE exchange | `GoogleCalendarClient` |
| `googleRefreshToken` | Google PKCE exchange | token refresh |
| `slackClientId` | user input | Slack OAuth start |
| `slackWebhookUrl` | Slack callback | `postToSlack()` |
| `slackChannel` | Slack callback | display in panel |

### GitHubClient token priority
`githubOAuthToken` → `token` (PAT). No other changes to `GitHubClient`.

### CSP update (`frontend/index.html` + `mcp/frontend/index.html`)
Add to `connect-src`:
```
https://accounts.google.com https://oauth2.googleapis.com https://www.googleapis.com https://hooks.slack.com
```

### Go MCP server env vars
| Var | Purpose |
|-----|---------|
| `GITHUB_CLIENT_SECRET` | GitHub code exchange |
| `SLACK_CLIENT_SECRET` | Slack code exchange |

---

## 3. GitHub OAuth

### Go server routes (`mcp/server/oauth.go`)
**`GET /oauth/github/start`**
- Query params: `client_id`
- Generates random `state` nonce, stores in a short-lived in-memory map (5 min TTL)
- Constructs `redirect_uri` as `http://{r.Host}/oauth/github/callback`
- Returns HTTP redirect to:
  ```
  https://github.com/login/oauth/authorize?client_id=X&redirect_uri=http://localhost:PORT/oauth/github/callback&state=Z&scope=repo
  ```

**`GET /oauth/github/callback`**
- Receives `code` + `state` from GitHub
- Validates `state` against stored nonce (rejects if missing/expired)
- POSTs to `https://github.com/login/oauth/access_token` with `client_id`, `client_secret` (from env), `code`
- On success, redirects to `{app_origin}/#gh_token={access_token}`
- On error, redirects to `{app_origin}/#gh_error={message}`

### Frontend (`frontend/src/services/oauth.js`)
- `startGitHubOAuth(clientId)` — builds `/oauth/github/start?client_id=X&redirect_uri=Y`, redirects
- `readOAuthFragment()` — parses `location.hash` for `gh_token`, `gh_error`, `slack_webhook`, `slack_channel`, Google `?code=` URL params; returns structured result; clears hash/params

### `app.js` bootstrap
After DOM ready, call `readOAuthFragment()`. If result contains `gh_token`, save to config as `githubOAuthToken`, verify via `gh.getUser()`, show toast "GitHub connected as `login`".

### Setup page
PAT field label updated: "Personal Access Token *(or connect via OAuth in Connections)*". Field remains fully functional.

---

## 4. Google Calendar OAuth (PKCE)

### PKCE flow (`frontend/src/services/oauth.js`)
`startGoogleOAuth(clientId)`:
1. Generate 32-byte random `code_verifier` (base64url)
2. SHA-256 hash → `code_challenge`
3. Store `{ verifier, state }` in `sessionStorage` under `oauth_google_pending`
4. Redirect to:
   ```
   https://accounts.google.com/o/oauth2/v2/auth
     ?client_id=X
     &redirect_uri={current_origin}
     &response_type=code
     &scope=https://www.googleapis.com/auth/calendar.readonly
     &code_challenge=Y
     &code_challenge_method=S256
     &state=Z
     &access_type=offline
     &prompt=consent
   ```

`exchangeGoogleCode(code, state)`:
1. Read `oauth_google_pending` from `sessionStorage`, verify state
2. POST to `https://oauth2.googleapis.com/token` with `code`, `client_id`, `code_verifier`, `redirect_uri`, `grant_type=authorization_code`
3. Save `googleAccessToken` + `googleRefreshToken` to config

`refreshGoogleToken(config)` — exchanges `googleRefreshToken` for new `googleAccessToken`.

### Google Calendar client (`frontend/src/services/google-calendar.js`)
`listTodayMeetings(accessToken)`:
- GET `https://www.googleapis.com/calendar/v3/calendars/primary/events`
- Params: `timeMin=now`, `timeMax=end-of-day`, `singleEvents=true`, `orderBy=startTime`, `maxResults=10`
- Returns array of `{ title, start, attendees }` (extracts `summary`, `start.dateTime`, `attendees[].email`)
- On 401: calls `refreshGoogleToken`, retries once

### Meeting drawer integration
When `googleAccessToken` present, `renderNotesTab()` shows a "Today's meetings" `<select>` above the drop zone. Selecting an event populates title, date, attendees. Falls back gracefully (no select rendered) if not connected.

---

## 5. Slack OAuth + Posting

### Go server routes (`mcp/server/oauth.go`)
**`GET /oauth/slack/start`**
- Query params: `client_id`
- Generates state nonce
- Constructs `redirect_uri` as `http://{r.Host}/oauth/slack/callback`
- Redirects to:
  ```
  https://slack.com/oauth/v2/authorize
    ?client_id=X
    &redirect_uri=http://localhost:PORT/oauth/slack/callback
    &scope=incoming-webhook
    &state=Z
  ```

**`GET /oauth/slack/callback`**
- Validates state
- POSTs to `https://slack.com/api/oauth.v2.access` with `client_id`, `client_secret`, `code`, `redirect_uri`
- Extracts `incoming_webhook.url` + `incoming_webhook.channel`
- Redirects to `{app_origin}/#slack_webhook={url}&slack_channel={channel}`

### Slack poster (`frontend/src/services/slack.js`)
```js
export async function postToSlack(webhookUrl, text) {
  const r = await fetch(webhookUrl, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text }),
  });
  if (!r.ok) throw new Error(`Slack post failed: ${r.status}`);
}
```

### Post triggers

**Meeting drawer — after successful Save:**
If `slackWebhookUrl` set, a "Post to Slack" button appears in save status area (opt-in):
```
*Meeting saved: {title} — {date}*
Notes: {notes}
Actions: {actions}
```

**Meeting drawer — after successful "Save to repo" (requirements):**
If `slackWebhookUrl` set, "Post to Slack" button appears:
```
*Requirements generated: {slug} — {date}*
{first 500 chars of requirements}…
```

Both buttons disappear after posting (show "Posted ✓" then fade).

---

## 6. File Map

| Action | Path | Purpose |
|--------|------|---------|
| Create | `frontend/src/services/oauth.js` | PKCE utils, OAuth start/exchange, fragment reader |
| Create | `frontend/src/services/google-calendar.js` | Calendar API wrapper, token refresh |
| Create | `frontend/src/services/slack.js` | Webhook poster |
| Create | `frontend/src/pages/connections.js` | Connections panel UI + cog init |
| Create | `mcp/server/oauth.go` | GitHub + Slack OAuth handlers + state nonce store; routes registered in `handlers.go` `routes()` |
| Modify | `frontend/src/app.js` | Call `readOAuthFragment()` on bootstrap, init cog |
| Modify | `frontend/src/pages/setup.js` | Update PAT label with OAuth hint |
| Modify | `frontend/src/components/meeting-drawer.js` | Add Google Calendar dropdown, Slack post buttons |
| Modify | `frontend/src/ui/main.css` | Cog + connections panel styles |
| Modify | `frontend/index.html` | Update CSP connect-src |
| Mirror | `mcp/frontend/src/` + `mcp/frontend/index.html` | Apply identical frontend changes |

---

## 7. Key Invariants

- PAT continues working unchanged — OAuth is additive, never required
- Go MCP server only needed during auth setup; day-to-day use is PAT or stored OAuth token
- All tokens stored only in `localStorage` under existing `team-memory:config` key
- Google Calendar dropdown degrades gracefully — meeting drawer fully functional without it
- Slack posting is always opt-in (button, not automatic)
- No new Go server endpoints beyond OAuth routes — existing MCP functionality unchanged
- Both `frontend/` and `mcp/frontend/` updated in sync (manual mirror, same as meeting PM feature)

---

## 8. Out of Scope (Phase 1)

- Token expiry UI (beyond inline refresh-on-401 for Google)
- Multiple Slack workspaces
- Google Calendar write (creating events)
- GitHub org/team scopes
