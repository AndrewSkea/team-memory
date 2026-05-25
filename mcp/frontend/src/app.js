import { renderSetup } from "./pages/setup.js";
import { renderRemember } from "./pages/remember.js";
import { renderLookup } from "./pages/lookup.js";
import { renderStats } from "./pages/stats.js";
import { renderStale } from "./pages/stale.js";
import { renderMeetings } from './pages/meetings.js';
import { renderProjects } from './pages/projects.js';
import { renderDecisions } from './pages/decisions.js';
import { renderActions } from './pages/actions.js';
import { renderTopics } from './pages/topics.js';
import { renderTimeline } from './pages/timeline.js';
import { GitHubClient } from './services/github.js';
import { initMeetingDrawer } from './components/meeting-drawer.js';
import { readOAuthFragment, exchangeGoogleCode } from './services/oauth.js';
import { initConnectionsCog } from './pages/connections.js';

const CONFIG_KEY = "team-memory:config";

function loadConfig() {
  try { return JSON.parse(localStorage.getItem(CONFIG_KEY)) || {}; } catch { return {}; }
}
function saveConfig(c) { localStorage.setItem(CONFIG_KEY, JSON.stringify(c)); }

export function toast(msg, isError = false) {
  const t = document.createElement("div");
  t.className = "toast" + (isError ? " error" : "");
  t.textContent = msg;
  document.body.appendChild(t);
  setTimeout(() => t.remove(), 3500);
}

const root = document.getElementById("root");
const nav = document.getElementById("nav");
const footer = document.getElementById("footer");

function updateFooter(config) {
  if (config.owner && config.repo) {
    footer.innerHTML = `<span class="footer-dot connected"></span>${config.owner} · ${config.owner}/${config.repo}`;
  } else {
    footer.innerHTML = `<span class="footer-dot"></span><button id="forget-auth-footer" style="background:none;border:none;color:var(--muted);font-size:12px;cursor:pointer;padding:0;line-height:1;vertical-align:middle;">Forget auth</button>`;
    document.getElementById("forget-auth-footer")?.addEventListener("click", forgetAuth);
  }
}

function forgetAuth() {
  if (confirm("Clear saved credentials? You will need to re-enter them.")) {
    localStorage.removeItem(CONFIG_KEY);
    go("remember");
  }
}

export function go(page) {
  const config = loadConfig();
  if (page !== "setup" && (!config.token || !config.owner)) {
    page = "setup";
  }
  for (const b of nav.querySelectorAll("button[data-page]")) {
    b.classList.toggle("active", b.dataset.page === page);
  }
  updateFooter(config);
  const gh = new GitHubClient({ ...config, token: config.githubOAuthToken || config.token });
  if (page === "setup") {
    renderSetup(root, {
      config,
      onDone: c => { saveConfig(c); toast("Setup saved."); go("remember"); },
    });
  } else if (page === "remember") {
    renderRemember(root, { config, toast, forgetAuth });
  } else if (page === "lookup") {
    renderLookup(root, { config, toast });
  } else if (page === "stats") {
    renderStats(root, { config, toast });
  } else if (page === "stale") {
    renderStale(root, { config, toast });
  } else if (page === "meetings") {
    renderMeetings(root, config, gh);
  } else if (page === "projects") {
    renderProjects(root, config, gh);
  } else if (page === "decisions") {
    renderDecisions(root, config, gh);
  } else if (page === "actions") {
    renderActions(root, config, gh);
  } else if (page === "topics") {
    renderTopics(root, config, gh);
  } else if (page === "timeline") {
    renderTimeline(root, config, gh);
  } else {
    renderLookup(root, { config, toast });
  }
}

nav.addEventListener("click", e => {
  const b = e.target.closest("button[data-page]");
  if (b) go(b.dataset.page);
});

// Bootstrap: fetch health (version) + pre-populate localStorage from CLI config on first visit
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
  } catch {}
  if (!loadConfig().token) {
    try {
      const r = await fetch("/v1/config");
      if (r.ok) {
        const cfg = await r.json();
        if (cfg.token && cfg.owner && cfg.repo) { saveConfig(cfg); }
      }
    } catch {}
  }
  // Handle OAuth callbacks before first render
  const oauthResult = readOAuthFragment();
  if (oauthResult) {
    const cfg = loadConfig();
    if (oauthResult.type === 'github') {
      cfg.githubOAuthToken = oauthResult.token;
      saveConfig(cfg);
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
    initMeetingDrawer(cfg, new GitHubClient({ ...cfg, token: cfg.githubOAuthToken || cfg.token }));
  }
  initConnectionsCog(cfg, (updated) => { saveConfig(updated); });
}
bootstrap();
