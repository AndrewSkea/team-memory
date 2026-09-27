import { api } from "../services/api.js";
import { escapeHtml } from "../services/html.js";

function cell(label, value, sub = "") {
  return `
    <div class="stat-cell">
      <div class="stat-value">${escapeHtml(String(value))}</div>
      <div class="stat-label">${escapeHtml(label)}</div>
      ${sub ? `<div class="stat-sub">${escapeHtml(sub)}</div>` : ""}
    </div>`;
}

function bars(title, rows, total) {
  if (!rows.length) return "";
  return `<div class="section-label bars-title">${escapeHtml(title)}</div>` + rows.map(([label, n]) => {
    const pct = total > 0 ? Math.round((n / total) * 100) : 0;
    return `
    <div class="bar-row">
      <div class="bar-meta"><span>${escapeHtml(label)}</span><span>${n}</span></div>
      <div class="bar-track"><div class="bar-fill" style="width:${pct}%"></div></div>
    </div>`;
  }).join("");
}

const SOURCES = { "session-end": "Captured at session end", precompact: "Captured before compaction", ui: "Web UI", mcp: "Claude (remember tool)", cli: "Command line", legacy: "Legacy entries", unknown: "Other" };

export async function renderStats(root) {
  root.innerHTML = `<div class="card"><div class="section-label">Team memory</div><div id="stats-body"><p class="muted">Loading…</p></div></div>`;
  const body = root.querySelector("#stats-body");
  try {
    const s = await api.stats();
    const auto = (s.by_source["session-end"] ?? 0) + (s.by_source.precompact ?? 0);
    const pct = s.active > 0 ? Math.round((auto / s.active) * 100) : 0;
    body.innerHTML = `
      <div class="stats-grid">
        ${cell("Active memories", s.active)}
        ${cell("Added this week", s.this_week)}
        ${cell("Captured automatically", auto, `${pct}% of active`)}
        ${cell("Systems known", s.entities)}
        ${cell("Waiting for you", s.pending)}
        ${cell("Retired", s.total - s.active - s.legacy, "superseded or retracted")}
      </div>
      ${bars("Most-documented systems", (s.top_entities ?? []).map(c => [c.name, c.count]), s.top_entities?.[0]?.count ?? 0)}
      ${bars("Contributors", (s.contributors ?? []).map(c => [c.name, c.count]), s.contributors?.[0]?.count ?? 0)}
      ${bars("How memories arrive", Object.entries(s.by_source).sort((a, b) => b[1] - a[1]).map(([k, n]) => [SOURCES[k] ?? k, n]), s.active)}
    `;
  } catch (e) {
    body.innerHTML = `<p class="error-text">Failed to load: ${escapeHtml(e.message)}</p>`;
  }
}
