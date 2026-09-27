import { api } from "../services/api.js";
import { escapeHtml } from "../services/html.js";
import { typeLabel, splitList, ago } from "../lib/view.js";

function item(p) {
  const m = p.memory;
  const id = escapeHtml(m.id);
  if (p.kind === "correction") {
    return `
    <article class="card pending" data-id="${id}">
      <div class="memory-head"><span class="type type-incident">Correction</span><h3>${escapeHtml(m.title)}</h3></div>
      <p class="why">${escapeHtml(p.reason)}</p>
      <div class="button-row">
        <button class="btn-primary" data-approve="${id}">Retract it</button>
        <button class="btn-outline" data-reject="${id}">Keep it</button>
      </div>
    </article>`;
  }
  return `
  <article class="card pending" data-id="${id}">
    <div class="memory-head"><span class="type type-${escapeHtml(m.type)}">${escapeHtml(typeLabel(m.type))}</span>
      <span class="muted small">${escapeHtml(ago(p.queued_at))}${m.learned_in ? " · from " + escapeHtml(m.learned_in) : ""}</span></div>
    <p class="why">Held because: ${escapeHtml(p.reason)}</p>
    <label class="field-label">Title</label>
    <input class="search-input" data-f="title" value="${escapeHtml(m.title)}">
    <label class="field-label">Summary</label>
    <input class="search-input" data-f="summary" value="${escapeHtml(m.summary ?? "")}">
    <label class="field-label">Details</label>
    <textarea data-f="body">${escapeHtml(m.body ?? "")}</textarea>
    <label class="field-label">About (systems)</label>
    <input class="search-input" data-f="about" value="${escapeHtml((m.about ?? []).join(", "))}">
    <div class="button-row">
      <button class="btn-primary" data-approve="${id}">Share with team</button>
      <button class="btn-outline" data-reject="${id}">Discard</button>
    </div>
  </article>`;
}

export async function renderReview(root, { toast, refreshSettings, go }) {
  root.innerHTML = `<div class="card"><p class="muted">Loading…</p></div>`;
  let items;
  try {
    ({ items } = await api.pending());
  } catch (e) {
    root.innerHTML = `<div class="card error-card">${escapeHtml(e.message)}</div>`;
    return;
  }
  if (!items.length) {
    root.innerHTML = `<div class="card empty"><h2>Nothing to review</h2>
      <p class="muted">Knowledge captured from your sessions that needs a human check will appear here. Change how much is held for review in Settings.</p></div>`;
    return;
  }
  root.innerHTML = `
    <div class="card review-head">
      <div><h2>${items.length} waiting for review</h2>
      <p class="muted small">Captured from your Claude Code sessions. Nothing here has been shared yet.</p></div>
      <button class="btn-outline" id="approve-all">Share all</button>
    </div>
    ${items.map(item).join("")}`;

  const done = async (el, msg) => {
    el.remove();
    toast(msg);
    await refreshSettings();
    if (!root.querySelector("article.pending")) go("review");
  };

  root.addEventListener("click", async e => {
    const a = e.target.closest("[data-approve]");
    const r = e.target.closest("[data-reject]");
    if (!a && !r) return;
    const card = e.target.closest("article");
    const id = (a || r).dataset.approve || (a || r).dataset.reject;
    card.querySelectorAll("button").forEach(b => b.disabled = true);
    try {
      if (a) {
        const edits = {};
        card.querySelectorAll("[data-f]").forEach(f => {
          edits[f.dataset.f] = f.dataset.f === "about" ? splitList(f.value) : f.value;
        });
        const m = await api.approve(id, edits);
        await done(card, `Shared: ${m.title}`);
      } else {
        await api.reject(id);
        await done(card, "Discarded.");
      }
    } catch (err) {
      toast(err.message, true);
      card.querySelectorAll("button").forEach(b => b.disabled = false);
    }
  });

  root.querySelector("#approve-all").onclick = async () => {
    if (!confirm(`Share all ${items.length} with your team as they are?`)) return;
    let ok = 0;
    for (const p of items) {
      try { await api.approve(p.memory.id, {}); ok++; } catch (e) { toast(`${p.memory.title}: ${e.message}`, true); }
    }
    toast(`Shared ${ok} memor${ok === 1 ? "y" : "ies"}.`);
    await refreshSettings();
    go("review");
  };
}
