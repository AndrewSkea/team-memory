import { api } from "../services/api.js";
import { escapeHtml } from "../services/html.js";
import { memoryCard, TYPES } from "../lib/view.js";

export async function renderSearch(root, { settings, toast }) {
  root.innerHTML = `
    <div class="card">
      <div class="search-wrap">
        <svg viewBox="0 0 16 16"><circle cx="6.5" cy="6.5" r="4.5"/><line x1="10.5" y1="10.5" x2="14" y2="14"/></svg>
        <input id="q" class="search-input" type="search" aria-label="Search team memory" placeholder="Search everything your team knows…" autocomplete="off">
      </div>
      <div class="filter-row">
        <select id="type" aria-label="Type"><option value="">All types</option>${TYPES.map(([v, l]) => `<option value="${v}">${l}</option>`).join("")}</select>
        <label class="checkbox-row"><input type="checkbox" id="all"> Include retracted/superseded</label>
      </div>
      <div id="entities" class="chips entity-bar" aria-label="Systems"></div>
    </div>
    <div id="results" aria-live="polite"></div>
  `;
  const $ = sel => root.querySelector(sel);
  let about = [];
  let timer;

  async function run() {
    const q = $("#q").value.trim();
    const out = $("#results");
    try {
      const { results: found } = await api.search({ q, about, type: $("#type").value, all: $("#all").checked, limit: 50 });
      const results = found ?? [];
      const heading = q || about.length ? `${results.length} result${results.length === 1 ? "" : "s"}` : "Recent memories";
      out.innerHTML = results.length
        ? `<div class="section-label list-heading">${heading}${about.length ? ` about <strong>${escapeHtml(about.join(", "))}</strong> <button class="link" id="clear-about">clear</button>` : ""}</div>` +
          results.map(r => memoryCard(r.memory, { settings })).join("")
        : `<div class="card empty">${q || about.length ? "No memories match. Try different words, or fewer filters." :
            "No memories yet. They appear here as your team works with Claude Code, or when someone saves one."}</div>`;
      out.querySelector("#clear-about")?.addEventListener("click", () => { about = []; run(); });
    } catch (e) {
      out.innerHTML = `<div class="card error-card">${escapeHtml(e.message)}</div>`;
    }
  }

  $("#q").addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(run, 180); });
  $("#type").onchange = run;
  $("#all").onchange = run;

  root.addEventListener("click", async e => {
    const chip = e.target.closest("[data-about]");
    if (chip) {
      about = [chip.dataset.about];
      run();
      return;
    }
    const f = e.target.closest("[data-forget]");
    if (f) {
      const reason = prompt("Why should this be forgotten? (It stays in git history but won't be recalled.)", "No longer accurate");
      if (reason === null) return;
      try {
        await api.forget(f.dataset.forget, reason);
        toast("Forgotten.");
        run();
      } catch (err) { toast(err.message, true); }
    }
  });

  try {
    const { entities } = await api.entities();
    if (entities.length) {
      $("#entities").innerHTML = `<span class="muted small">Systems:</span>` + entities.slice(0, 30)
        .map(en => `<button class="chip" data-about="${escapeHtml(en.name)}" title="${escapeHtml(en.description || "")}">${escapeHtml(en.name)}</button>`).join("");
    }
  } catch { /* optional */ }
  $("#q").focus();
  run();
}
