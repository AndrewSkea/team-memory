import { GitHubClient } from "../services/github.js";
import { Cache } from "../services/cache.js";
import { computeStats } from "../services/stats.js";

export function renderStats(root, { config, toast }) {
  root.innerHTML = `
    <div class="card">
      <h2>Stats</h2>
      <div id="stats-body">
        <p class="muted">Loading…</p>
      </div>
    </div>
  `;

  const gh = new GitHubClient({ token: config.token, owner: config.owner, repo: config.repo });
  const cache = new Cache("team-memory");

  computeStats(gh, cache).then(s => {
    const autoCaptures = s.bySource.Stop + s.bySource.PreCompact;
    const pct = s.total > 0 ? Math.round((autoCaptures / s.total) * 100) : 0;
    document.getElementById("stats-body").innerHTML = `
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:12px;margin-bottom:16px;">
        ${stat("Total memories", s.total)}
        ${stat("This week", s.thisWeek)}
        ${stat("Auto-captured", autoCaptures, `${pct}% of total`)}
        ${stat("Manual (UI)", s.bySource.UI)}
        ${stat("Topics covered", s.filesCount + " files")}
        ${stat("Lookups run", s.lookups)}
      </div>
      <div style="margin-top:8px;">
        <p class="muted" style="margin:0 0 4px;">By source</p>
        ${bar("Stop hook", s.bySource.Stop, s.total, "#c84e1a")}
        ${bar("PreCompact hook", s.bySource.PreCompact, s.total, "#8b6914")}
        ${bar("UI", s.bySource.UI, s.total, "#4a7c59")}
      </div>
      <p class="muted" style="margin-top:16px;font-size:12px;">
        Repo: <a href="https://github.com/${config.owner}/${config.repo}" target="_blank">${config.owner}/${config.repo}</a>
      </p>
    `;
  }).catch(e => {
    document.getElementById("stats-body").innerHTML = `<p class="muted" style="color:var(--danger)">Failed to load: ${e.message}</p>`;
  });
}

function stat(label, value, sub = "") {
  return `
    <div style="background:var(--bg);border-radius:8px;padding:12px;">
      <div style="font-size:24px;font-weight:600;color:var(--accent)">${value}</div>
      <div style="font-size:13px;color:var(--text)">${label}</div>
      ${sub ? `<div class="muted" style="font-size:11px;">${sub}</div>` : ""}
    </div>`;
}

function bar(label, count, total, color) {
  const pct = total > 0 ? Math.round((count / total) * 100) : 0;
  return `
    <div style="margin-bottom:6px;">
      <div style="display:flex;justify-content:space-between;font-size:12px;margin-bottom:2px;">
        <span>${label}</span><span>${count} (${pct}%)</span>
      </div>
      <div style="background:var(--border);border-radius:3px;height:6px;">
        <div style="width:${pct}%;background:${color};height:6px;border-radius:3px;transition:width 0.3s;"></div>
      </div>
    </div>`;
}
