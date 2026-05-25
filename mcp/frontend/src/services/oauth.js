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
  sessionStorage.removeItem('oauth_google_pending');
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

  if (hash) {
    const params = new URLSearchParams(hash.slice(1));
    if (params.get('gh_token')) return { type: 'github', token: params.get('gh_token') };
    if (params.get('gh_error')) return { type: 'github_error', error: params.get('gh_error') };
    if (params.get('slack_webhook')) return {
      type: 'slack',
      webhookUrl: params.get('slack_webhook'),
      channel: params.get('slack_channel') || '',
    };
  }

  if (search) {
    const params = new URLSearchParams(search.slice(1));
    if (params.get('code') && params.get('state')) {
      return { type: 'google', code: params.get('code'), state: params.get('state') };
    }
  }

  return null;
}
