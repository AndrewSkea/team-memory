// Pure view helpers (unit-tested in node).
import { escapeHtml } from "../services/html.js";

export const TYPES = [
  ["note", "Note"], ["decision", "Decision"], ["gotcha", "Gotcha"], ["howto", "How-to"],
  ["fact", "Fact"], ["convention", "Convention"], ["incident", "Incident"], ["meeting", "Meeting"],
  ["action", "Action"], ["reminder", "Reminder"], ["idea", "Idea"],
];

export function typeLabel(t) {
  return (TYPES.find(([k]) => k === t) ?? [t, t || "Note"])[1];
}

export function ago(iso, now = new Date()) {
  const t = new Date(iso);
  if (isNaN(t) || t.getFullYear() < 2000) return "";
  const s = (now - t) / 1000;
  if (s < 120) return "just now";
  const m = s / 60, h = m / 60, d = h / 24;
  if (m < 60) return `${Math.floor(m)} minutes ago`;
  if (h < 48) return `${Math.floor(h)} hours ago`;
  if (d < 14) return `${Math.floor(d)} days ago`;
  if (d < 60) return `${Math.floor(d / 7)} weeks ago`;
  if (d < 730) return `${Math.floor(d / 30)} months ago`;
  return `${Math.floor(d / 365)} years ago`;
}

export function githubUrl(settings, path) {
  if (!settings?.owner || !settings?.repo || !path) return "";
  const segs = path.split("/").map(encodeURIComponent).join("/");
  return `https://github.com/${encodeURIComponent(settings.owner)}/${encodeURIComponent(settings.repo)}/blob/HEAD/${segs}`;
}

export function provenance(m, now) {
  const bits = [];
  if (m.learned_in) bits.push(`learned in ${m.learned_in}`);
  if (m.author) bits.push(`by ${m.author}`);
  const when = ago(m.created, now);
  if (when) bits.push(when);
  return bits.join(" · ");
}

// memoryCard renders one memory. Every interpolated value is escaped.
export function memoryCard(m, { settings, now, actions = true } = {}) {
  const about = (m.about ?? []).map(a =>
    `<button class="chip" data-about="${escapeHtml(a)}" title="Show everything about ${escapeHtml(a)}">${escapeHtml(a)}</button>`).join("");
  const url = githubUrl(settings, m.path);
  const inactive = m.status && m.status !== "active";
  return `
  <article class="memory${inactive ? " inactive" : ""}" data-id="${escapeHtml(m.id)}">
    <div class="memory-head">
      <span class="type type-${escapeHtml(m.type || "note")}">${escapeHtml(typeLabel(m.type))}</span>
      <h3>${escapeHtml(m.title)}</h3>
    </div>
    ${m.summary ? `<p class="summary">${escapeHtml(m.summary)}</p>` : ""}
    ${m.body && m.body !== m.summary ? `<details><summary>Details</summary><pre class="body">${escapeHtml(m.body)}</pre></details>` : ""}
    ${inactive ? `<p class="status">${escapeHtml(m.status)}${m.status_reason ? ": " + escapeHtml(m.status_reason) : ""}</p>` : ""}
    <div class="memory-foot">
      <div class="chips">${about}</div>
      <div class="meta">${escapeHtml(provenance(m, now))}${m.legacy ? " · legacy" : ""}${m.scope === "personal" ? " · personal" : ""}</div>
    </div>
    ${actions ? `<div class="memory-actions">
      ${url ? `<a href="${escapeHtml(url)}" target="_blank" rel="noopener noreferrer">View on GitHub</a>` : ""}
      ${!m.legacy && !inactive ? `<button class="link danger" data-forget="${escapeHtml(m.id)}">Forget</button>` : ""}
    </div>` : ""}
  </article>`;
}

export function splitList(s) {
  return String(s ?? "").split(",").map(x => x.trim()).filter(Boolean);
}
