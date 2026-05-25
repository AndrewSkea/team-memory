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
    const panelBody = div.closest('.connections-panel__body');
    if (panelBody) renderConnections(panelBody, config, onUpdate);
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
