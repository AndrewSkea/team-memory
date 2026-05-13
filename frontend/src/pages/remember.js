import { GitHubClient } from "../services/github.js";
import { Cache } from "../services/cache.js";
import { parseIndex, serializeIndex, upsertEntry } from "../services/indexmd.js";
import { renderEntry } from "../services/entries.js";
import { pickBackend } from "../services/llm/backend.js";
import { formatDecision } from "./decisions.js";
import { formatAction } from "./actions.js";
import { formatReminderEntry } from "./reminders.js";
import { parseMsgFile } from "../services/msgparser.js";

const TYPES = ["General", "Decision", "Action", "Reminder", "Meeting Notes", "Programming", "Ideas", "Unsure"];
const SPECIAL = new Set(["Decision", "Action", "Reminder"]);

export function renderRemember(root, { config, toast, forgetAuth }) {
  const gh = new GitHubClient({ token: config.token, owner: config.owner, repo: config.repo });
  const cache = new Cache("team-memory");
  const today = new Date().toISOString().slice(0, 10);

  root.innerHTML = `
    <div class="card">
      <div style="display:flex;align-items:center;gap:8px;margin-bottom:12px">
        <label style="font-size:12px;font-weight:600;color:var(--muted);white-space:nowrap">Type</label>
        <select id="type" class="search-input" style="flex:0 0 auto;width:auto">${TYPES.map(t => `<option>${t}</option>`).join('')}</select>
        <div id="scope-wrap" style="display:flex;align-items:center;gap:8px;margin-left:auto">
          <label style="font-size:12px;font-weight:600;color:var(--muted)">Scope</label>
          <select id="scope" class="search-input" style="width:auto"><option>auto</option><option>Team</option><option>Personal</option></select>
        </div>
      </div>

      <div id="section-general">
        <textarea id="text" placeholder="Describe a useful pattern, a gotcha, a workflow tip, or anything worth sharing with your team…"></textarea>
        <label class="checkbox-row" style="margin-top:8px">
          <input type="checkbox" id="unsure">
          Not sure — park this in UNSURE.md for later
        </label>
        <label class="attach-link">
          <svg viewBox="0 0 16 16"><path d="M13.5 7.5l-5.5 5.5a3.5 3.5 0 01-5-5l6-6a2 2 0 012.8 2.8L6 11a.7.7 0 01-1-1l5.5-5.5"/></svg>
          Attach a file
          <input id="file" type="file" accept=".txt,text/plain" style="display:none" />
        </label>
      </div>

      <div id="section-decision" style="display:none">
        <p class="section-label">Title</p>
        <input id="dtitle" class="search-input" type="text" placeholder="Use JWT for auth" style="width:100%;box-sizing:border-box;margin-bottom:10px">
        <div class="fields-row">
          <div>
            <p class="section-label">Date</p>
            <input id="ddate" class="search-input" type="date" value="${today}" style="width:100%;box-sizing:border-box">
          </div>
          <div>
            <p class="section-label">Status</p>
            <select id="dstatus" class="search-input" style="width:100%;box-sizing:border-box">
              <option>Proposed</option><option>Accepted</option><option>Deprecated</option><option>Superseded</option>
            </select>
          </div>
        </div>
        <p class="section-label" style="margin-top:10px">Context — why is this needed?</p>
        <textarea id="dcontext" class="textarea" placeholder="What problem are we solving?"></textarea>
        <p class="section-label">Decision — what did we decide?</p>
        <textarea id="ddecision" class="textarea" placeholder="We will…"></textarea>
        <p class="section-label">Consequences (one per line)</p>
        <textarea id="dconsequences" class="textarea" style="min-height:80px" placeholder="Clients must handle X"></textarea>
      </div>

      <div id="section-action" style="display:none">
        <p class="section-label">What needs to be done?</p>
        <input id="atext" class="search-input" type="text" placeholder="Update deployment docs" style="width:100%;box-sizing:border-box;margin-bottom:10px">
        <div class="fields-row">
          <div>
            <p class="section-label">Priority</p>
            <select id="apriority" class="search-input" style="width:100%;box-sizing:border-box">
              <option>HIGH</option><option selected>MEDIUM</option><option>LOW</option>
            </select>
          </div>
          <div>
            <p class="section-label">Owner (optional)</p>
            <input id="aowner" class="search-input" type="text" placeholder="Alice" style="width:100%;box-sizing:border-box">
          </div>
        </div>
        <p class="section-label" style="margin-top:10px">Due date (optional)</p>
        <input id="adue" class="search-input" type="date" style="width:200px">
      </div>

      <div id="section-reminder" style="display:none">
        <div class="drop-zone" id="rdrop">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="20" height="20" style="display:block;margin:0 auto 6px"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
          Drop an email (.msg) to auto-fill, or fill in manually
          <input type="file" accept=".msg" id="rfile">
        </div>
        <p class="section-label">What do you need to do?</p>
        <input id="rtitle" class="search-input" type="text" placeholder="Submit Q2 report" style="width:100%;box-sizing:border-box;margin-bottom:10px">
        <p class="section-label">Due date</p>
        <input id="rdate" class="search-input" type="date" value="${today}" style="width:100%;box-sizing:border-box;margin-bottom:10px">
        <p class="section-label">Details <span style="color:var(--muted);font-weight:400">(optional)</span></p>
        <textarea id="rdetails" class="textarea" placeholder="Context, links, notes…" style="min-height:80px"></textarea>
      </div>

      <div class="divider"></div>

      <div class="button-row">
        <button class="btn-primary" id="save">
          <svg viewBox="0 0 16 16"><path d="M14 2H9l-7 7 5 5 7-7V2z"/><circle cx="12" cy="4" r="1.2" fill="currentColor" stroke="none"/></svg>
          <span id="save-label">Save to memory</span>
        </button>
        <button class="btn-outline" id="raw">
          <svg viewBox="0 0 16 16"><path d="M2 2h9l3 3v9H2V2z"/><rect x="4" y="9" width="8" height="5" rx="1"/><rect x="4" y="2" width="6" height="4" rx="1"/></svg>
          Save without classification
        </button>
      </div>

      <div id="preview"></div>
    </div>
  `;

  const $ = sel => root.querySelector(sel);

  function onTypeChange() {
    const type = $("#type").value;
    const isSpecial = SPECIAL.has(type);
    $("#section-general").style.display   = isSpecial ? 'none' : '';
    $("#section-decision").style.display  = type === 'Decision' ? '' : 'none';
    $("#section-action").style.display    = type === 'Action'   ? '' : 'none';
    $("#section-reminder").style.display  = type === 'Reminder' ? '' : 'none';
    $("#scope-wrap").style.display        = isSpecial ? 'none' : '';
    $("#raw").style.display               = isSpecial ? 'none' : '';
    const labels = { Decision: 'Save decision', Action: 'Add action', Reminder: 'Save reminder' };
    $("#save-label").textContent = labels[type] ?? 'Save to memory';
    if (type === 'Unsure') $("#unsure").checked = true;
    else if (!isSpecial) $("#unsure").checked = false;
  }

  $("#type").onchange = onTypeChange;
  onTypeChange();

  $("#unsure").onchange = () => {
    if ($("#unsure").checked) $("#type").value = "Unsure";
  };

  $("#file").onchange = async (e) => {
    const f = e.target.files[0];
    if (!f) return;
    if (f.size > 1024 * 1024) { toast("File too large (max 1MB)", true); return; }
    $("#text").value = await f.text();
  };

  // .msg drag-drop for Reminder
  function handleMsgFile(file) {
    if (!file || !file.name.endsWith('.msg')) return;
    file.arrayBuffer().then(buf => {
      try {
        const { subject, date, body } = parseMsgFile(buf);
        if (subject) $("#rtitle").value = subject;
        if (date) $("#rdate").value = date.toISOString().slice(0, 10);
        if (body) $("#rdetails").value = body.trim().slice(0, 1000);
      } catch {
        toast("Could not parse .msg file", true);
      }
    });
  }
  const drop = $("#rdrop");
  drop.addEventListener('dragover', e => { e.preventDefault(); drop.classList.add('drag-over'); });
  drop.addEventListener('dragleave', () => drop.classList.remove('drag-over'));
  drop.addEventListener('drop', e => { e.preventDefault(); drop.classList.remove('drag-over'); handleMsgFile(e.dataTransfer.files[0]); });
  $("#rfile").addEventListener('change', e => handleMsgFile(e.target.files[0]));

  const SAVE_ICON = `<svg viewBox="0 0 16 16"><path d="M14 2H9l-7 7 5 5 7-7V2z"/><circle cx="12" cy="4" r="1.2" fill="currentColor" stroke="none"/></svg>`;
  const RAW_ICON  = `<svg viewBox="0 0 16 16"><path d="M2 2h9l3 3v9H2V2z"/><rect x="4" y="9" width="8" height="5" rx="1"/><rect x="4" y="2" width="6" height="4" rx="1"/></svg>`;

  async function saveDecision() {
    const title = $("#dtitle").value.trim();
    if (!title) { toast("Title required", true); return false; }
    const md = formatDecision({
      title,
      date: $("#ddate").value,
      status: $("#dstatus").value,
      context: $("#dcontext").value,
      decision: $("#ddecision").value,
      consequences: $("#dconsequences").value,
    });
    await gh.commitFile({ path: 'DECISIONS.md', append: md, message: `team-memory: decision — ${title}` });
    ['#dtitle','#dcontext','#ddecision','#dconsequences'].forEach(id => { const el = $(id); if (el) el.value = ''; });
    toast("Saved to DECISIONS.md");
    return true;
  }

  async function saveAction() {
    const text = $("#atext").value.trim();
    if (!text) { toast("Action text required", true); return false; }
    const md = formatAction({
      text,
      priority: $("#apriority").value,
      owner: $("#aowner").value,
      due: $("#adue").value,
    });
    await gh.commitFile({ path: 'ACTIONS.md', append: md, message: `team-memory: action — ${text.slice(0, 40)}` });
    ['#atext','#aowner'].forEach(id => { const el = $(id); if (el) el.value = ''; });
    const due = $("#adue"); if (due) due.value = '';
    toast("Saved to ACTIONS.md");
    return true;
  }

  async function saveReminder() {
    const title   = $("#rtitle").value.trim();
    const dueDate = $("#rdate").value;
    const details = $("#rdetails").value.trim();
    if (!title)   { toast("Title required", true); return false; }
    if (!dueDate) { toast("Due date required", true); return false; }
    const backend = await pickBackend({ anthropicKey: config.anthropicKey });
    let formatted;
    if (backend) {
      formatted = await backend.formatReminder({ title, dueDate, details });
    } else {
      formatted = { short_title: title.slice(0, 60), bullets: details ? [details.split('\n')[0].slice(0, 80)] : [], tags: '' };
    }
    const entry = formatReminderEntry(formatted, dueDate);
    await gh.commitFile({ path: `users/${config.owner}/REMINDERS.md`, append: '\n' + entry, message: `team-memory: reminder — ${formatted.short_title}` });
    ['#rtitle','#rdetails'].forEach(id => { const el = $(id); if (el) el.value = ''; });
    const rdate = $("#rdate"); if (rdate) rdate.value = new Date().toISOString().slice(0, 10);
    toast("Saved to REMINDERS.md");
    return true;
  }

  async function commitEntry(preset) {
    const text = $("#text").value.trim();
    if (!text) { toast("Empty memory.", true); return false; }
    const type = $("#type").value;
    const scope = resolveScope($("#scope").value, config.owner);
    const useUnsure = $("#unsure").checked || type === "Unsure" || preset?.unsure;
    const target = preset?.target_file ?? (useUnsure ? "UNSURE.md" : "GENERAL.md");
    const entry = {
      timestamp: new Date().toISOString(),
      shortTitle: preset?.short_title ?? text.split("\n")[0].slice(0, 60),
      scope, type,
      tags: preset?.tags ?? "",
      source: "UI",
      summary: preset?.one_sentence_summary ?? "",
      bullets: preset?.bullets ?? [],
      full: text,
    };
    const res = await gh.commitFile({ path: target, append: "\n" + renderEntry(entry), message: `team-memory: add entry to ${target}` });
    if (!res.ok) {
      if (res.kind === "conflict") toast("File changed on GitHub — try again.", true);
      else if (res.kind === "auth") toast("Auth failed — check your PAT.", true);
      else toast(`Save failed (${res.kind}): ${res.message ?? ""}`, true);
      return false;
    }
    const idxFile = await getIndex(gh, cache, true);
    const idx = parseIndex(idxFile.content);
    if (!idx.entries.find(e => e.path === target)) {
      const scopeStr = target.startsWith("users/") ? `personal:${config.owner}` : "shared";
      upsertEntry(idx, { path: target, scope: scopeStr, topics: preset?.tags ?? type.toLowerCase() });
      await gh.putContent({ path: "INDEX.md", content: serializeIndex(idx), message: "team-memory: update INDEX.md" });
    }
    await cache.delete("file:" + target);
    await cache.delete("file:INDEX.md");
    $("#text").value = "";
    $("#preview").innerHTML = "";
    toast(`Saved to ${target}.`);
    return true;
  }

  $("#save").onclick = async () => {
    const type = $("#type").value;
    const saveBtn = $("#save");
    const origLabel = $("#save-label").textContent;
    saveBtn.disabled = true;
    $("#raw").disabled = true;
    try {
      if (type === 'Decision') {
        $("#save-label").textContent = 'Saving…';
        await saveDecision();
      } else if (type === 'Action') {
        $("#save-label").textContent = 'Saving…';
        await saveAction();
      } else if (type === 'Reminder') {
        $("#save-label").textContent = 'Saving…';
        await saveReminder();
      } else {
        const text = $("#text").value.trim();
        if (!text) { toast("Type or attach some text first.", true); return; }
        $("#save-label").textContent = 'Classifying…';
        const backend = await pickBackend({ anthropicKey: config.anthropicKey });
        if (!backend) { toast("No LLM available — add an Anthropic key in Setup or ensure the binary is running.", true); return; }
        const indexFile = await getIndex(gh, cache);
        const scope = resolveScope($("#scope").value, config.owner);
        $("#save-label").textContent = 'Saving…';
        const preset = await backend.categorize({
          index: indexFile.content,
          payload: { scope, type, text, source: "UI", timestamp: new Date().toISOString() },
        });
        await commitEntry(preset);
      }
    } catch (e) {
      toast("Save failed: " + e.message, true);
    } finally {
      $("#save-label").textContent = origLabel;
      saveBtn.disabled = false;
      $("#raw").disabled = false;
    }
  };

  $("#raw").onclick = async () => {
    const saveBtn = $("#save");
    const rawBtn  = $("#raw");
    const text = $("#text").value.trim();
    if (!text) { toast("Type or attach some text first.", true); return; }

    async function doRaw() {
      saveBtn.disabled = true;
      rawBtn.disabled  = true;
      rawBtn.innerHTML = `${RAW_ICON} Saving…`;
      try {
        await commitEntry(null);
      } catch (e) {
        toast("Save failed: " + e.message, true);
      } finally {
        rawBtn.innerHTML = `${RAW_ICON} Save without classification`;
        saveBtn.disabled = false;
        rawBtn.disabled  = false;
      }
    }

    if (config.check_first) {
      if (!root.querySelector("#check-first-confirm")) {
        const box = document.createElement("div");
        box.id = "check-first-confirm";
        box.className = "confirm-box";
        box.innerHTML = `Save as unstructured entry to GENERAL.md?
          <button id="cfyes" class="btn-primary" style="padding:6px 14px;font-size:13px;">Save</button>
          <button id="cfno" class="btn-outline" style="padding:6px 14px;font-size:13px;">Cancel</button>`;
        $("#preview").before(box);
        box.querySelector("#cfno").onclick = () => box.remove();
        box.querySelector("#cfyes").onclick = () => { box.remove(); doRaw(); };
      }
      return;
    }
    await doRaw();
  };
}

function resolveScope(sel, owner) {
  if (sel === "Team") return "Team";
  if (sel === "Personal") return `Personal:${owner}`;
  return "Team";
}

async function getIndex(gh, cache, forceFresh = false) {
  if (!forceFresh) {
    const cached = await cache.get("file:INDEX.md");
    if (cached) return cached;
  }
  const f = await gh.getFile("INDEX.md");
  await cache.set("file:INDEX.md", f);
  return f;
}
