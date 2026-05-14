import { GitHubClient } from "../services/github.js";
import { Cache } from "../services/cache.js";
import { computeStats } from "../services/stats.js";
import { escapeHtml } from "../services/html.js";

export function renderStats(root, { config, toast }) {
  root.innerHTML = `
    <div class="card">
      <div class="section-label">Memory bank stats</div>
      <div id="stats-body">
        <p style="color:var(--muted);font-size:14px;">Loading…</p>
      </div>
    </div>
  `;

  const gh = new GitHubClient({ token: config.token, owner: config.owner, repo: config.repo });
  const cache = new Cache("team-memory");

  computeStats(gh, cache).then(s => {
    const autoCaptures = s.bySource.Stop + s.bySource.PreCompact;
    const pct = s.total > 0 ? Math.round((autoCaptures / s.total) * 100) : 0;
    document.getElementById("stats-body").innerHTML = `
      <div class="stats-grid">
        ${cell("Total memories", s.total)}
        ${cell("This week", s.thisWeek)}
        ${cell("Auto-captured", autoCaptures, `${pct}% of total`)}
        ${cell("Manual (UI)", s.bySource.UI)}
        ${cell("Files", s.filesCount)}
        ${cell("Lookups", s.lookups)}
      </div>
      <div class="section-label" style="margin-top:16px;margin-bottom:10px;">By source</div>
      ${bar("Stop hook", s.bySource.Stop, s.total, "#c84e1a")}
      ${bar("PreCompact hook", s.bySource.PreCompact, s.total, "#8b6914")}
      ${bar("UI", s.bySource.UI, s.total, "#3d9e5e")}
      <p style="margin-top:14px;font-size:12px;color:var(--muted);">
        Repo: <a href="https://github.com/${encodeURIComponent(config.owner)}/${encodeURIComponent(config.repo)}" target="_blank" rel="noopener noreferrer">${escapeHtml(config.owner)}/${escapeHtml(config.repo)}</a>
      </p>
    `;
  }).catch(e => {
    document.getElementById("stats-body").innerHTML =
      `<p style="color:var(--danger);font-size:14px;">Failed to load: ${escapeHtml(e.message)}</p>`;
  });
}

function cell(label, value, sub = "") {
  return `
    <div class="stat-cell">
      <div class="stat-value">${value}</div>
      <div class="stat-label">${label}</div>
      ${sub ? `<div class="stat-sub">${sub}</div>` : ""}
    </div>`;
}

function bar(label, count, total, color) {
  const pct = total > 0 ? Math.round((count / total) * 100) : 0;
  return `
    <div class="bar-row">
      <div class="bar-meta"><span>${label}</span><span>${count} (${pct}%)</span></div>
      <div class="bar-track"><div class="bar-fill" style="width:${pct}%;background:${color};"></div></div>
    </div>`;
}
