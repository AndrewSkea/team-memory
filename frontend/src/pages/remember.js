import { api } from "../services/api.js";
import { escapeHtml } from "../services/html.js";
import { formatDecision, formatAction, formatReminder } from "../lib/formats.js";
import { memoryCard, splitList } from "../lib/view.js";

const KINDS = [
  ["", "Let AI decide"], ["gotcha", "Gotcha"], ["howto", "How-to"], ["fact", "Fact"], ["convention", "Convention"],
  ["decision", "Decision"], ["incident", "Incident"], ["action", "Action item"], ["reminder", "Reminder"], ["idea", "Idea"], ["note", "Note"],
];

export function renderRemember(root, { settings, toast, refreshSettings }) {
  const today = new Date().toISOString().slice(0, 10);
  root.innerHTML = `
    <div class="card">
      <div class="type-row">
        <label class="row-label" for="type">Type</label>
        <select id="type">${KINDS.map(([v, l]) => `<option value="${v}">${l}</option>`).join("")}</select>
        <label class="row-label" for="scope">Share with</label>
        <select id="scope"><option value="team">Team</option><option value="personal">Just me</option></select>
      </div>

      <div id="section-general" class="stack">
        <textarea id="text" aria-label="What should your team know?" placeholder="A gotcha, a decision and why, how you finally got something working… anything a teammate — or their AI assistant — should know next time."></textarea>
        <input id="about" class="search-input" type="text" autocomplete="off" placeholder="About which systems? (optional, comma-separated — AI fills this in)">
        <label class="attach-link">
          <svg viewBox="0 0 16 16"><path d="M13.5 7.5l-5.5 5.5a3.5 3.5 0 01-5-5l6-6a2 2 0 012.8 2.8L6 11a.7.7 0 01-1-1l5.5-5.5"/></svg>
          Attach a text file
          <input id="file" type="file" accept=".txt,.md,text/plain,text/markdown" hidden>
        </label>
      </div>

      <div id="section-decision" class="stack" hidden>
        <input id="dtitle" class="search-input" type="text" placeholder="Decision title, e.g. Use JWT for service auth">
        <div class="fields-row">
          <input id="ddate" class="search-input" type="date" value="${today}" aria-label="Date">
          <select id="dstatus" aria-label="Status"><option>Proposed</option><option selected>Accepted</option><option>Deprecated</option><option>Superseded</option></select>
        </div>
        <textarea id="dcontext" placeholder="Context — what problem are we solving?"></textarea>
        <textarea id="ddecision" placeholder="Decision — we will…"></textarea>
        <textarea id="dconsequences" placeholder="Consequences — one per line"></textarea>
      </div>

      <div id="section-action" class="stack" hidden>
        <input id="atext" class="search-input" type="text" placeholder="What needs to be done?">
        <div class="fields-row">
          <select id="apriority" aria-label="Priority"><option>HIGH</option><option selected>MEDIUM</option><option>LOW</option></select>
          <input id="aowner" class="search-input" type="text" placeholder="Owner (optional)">
          <input id="adue" class="search-input" type="date" aria-label="Due date">
        </div>
      </div>

      <div id="section-reminder" class="stack" hidden>
        <input id="rtitle" class="search-input" type="text" placeholder="What do you need to do?">
        <input id="rdate" class="search-input" type="date" value="${today}" aria-label="Due date">
        <textarea id="rdetails" placeholder="Details (optional)"></textarea>
      </div>

      <div class="divider"></div>
      <div class="button-row">
        <button class="btn-primary" id="save">Save to team memory</button>
        <button class="btn-outline" id="raw" title="Save exactly as written, without AI tidying">Save as written</button>
      </div>
      <p class="muted small">Secrets (tokens, passwords, keys) are removed automatically before anything is saved.</p>
    </div>
    <div id="saved"></div>
  `;
  const $ = sel => root.querySelector(sel);

  function section() {
    const t = $("#type").value;
    return t === "decision" || t === "action" || t === "reminder" ? t : "general";
  }
  function onType() {
    const s = section();
    for (const k of ["general", "decision", "action", "reminder"]) $(`#section-${k}`).hidden = s !== k;
    if (s === "reminder") $("#scope").value = "personal";
  }
  $("#type").onchange = onType;
  onType();

  $("#file").onchange = async e => {
    const f = e.target.files[0];
    if (!f) return;
    if (f.size > 512 * 1024) { toast("File too large (max 512 KB)", true); return; }
    $("#text").value = await f.text();
  };

  function collect() {
    const type = $("#type").value;
    const scope = $("#scope").value;
    const about = splitList($("#about").value);
    switch (section()) {
      case "decision": {
        const title = $("#dtitle").value.trim();
        if (!title) throw new Error("Give the decision a title.");
        const text = formatDecision({ title, date: $("#ddate").value, status: $("#dstatus").value,
          context: $("#dcontext").value, decision: $("#ddecision").value, consequences: $("#dconsequences").value });
        return { title, text, type, scope, about };
      }
      case "action": {
        const t = $("#atext").value.trim();
        if (!t) throw new Error("Describe the action.");
        return { title: t, text: formatAction({ text: t, priority: $("#apriority").value, owner: $("#aowner").value, due: $("#adue").value }), type, scope, about };
      }
      case "reminder": {
        const title = $("#rtitle").value.trim();
        if (!title) throw new Error("Describe the reminder.");
        return { title, text: formatReminder({ title, dueDate: $("#rdate").value, details: $("#rdetails").value }), type, scope, about };
      }
    }
    const text = $("#text").value.trim();
    if (!text) throw new Error("Write something to remember first.");
    return { text, type, scope, about };
  }

  function reset() {
    for (const id of ["#text", "#about", "#dtitle", "#dcontext", "#ddecision", "#dconsequences", "#atext", "#aowner", "#adue", "#rtitle", "#rdetails"]) {
      const el = $(id); if (el) el.value = "";
    }
  }

  async function save(classify) {
    let input;
    try { input = collect(); } catch (e) { toast(e.message, true); return; }
    const btn = classify ? $("#save") : $("#raw");
    const label = btn.textContent;
    $("#save").disabled = $("#raw").disabled = true;
    btn.textContent = classify ? "Saving…" : "Saving…";
    try {
      const res = await api.remember({ ...input, classify, source: "ui" });
      reset();
      (res.warnings ?? []).forEach(w => toast(w, true));
      toast("Saved — your team's assistants can now recall it.");
      $("#saved").innerHTML = `<div class="card"><div class="section-label">Saved</div>${memoryCard(res.memory, { settings, actions: false })}</div>`;
      refreshSettings();
    } catch (e) {
      toast("Save failed: " + e.message, true);
    } finally {
      btn.textContent = label;
      $("#save").disabled = $("#raw").disabled = false;
    }
  }
  $("#save").onclick = () => save(true);
  $("#raw").onclick = () => save(false);
  $("#text").addEventListener("keydown", e => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) save(true);
  });
  $("#text").focus();
}
