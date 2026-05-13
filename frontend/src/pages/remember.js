import { GitHubClient } from "../services/github.js";
import { Cache } from "../services/cache.js";
import { parseIndex, serializeIndex, upsertEntry } from "../services/indexmd.js";
import { renderEntry } from "../services/entries.js";
import { pickBackend } from "../services/llm/backend.js";

const TYPES = ["General", "Meeting Notes", "Programming", "Ideas", "Reminders", "Unsure"];

export function renderRemember(root, { config, toast, forgetAuth }) {
  const gh = new GitHubClient({ token: config.token, owner: config.owner, repo: config.repo });
  const cache = new Cache("team-memory");

  root.innerHTML = `
    <div class="card">
      <div class="section-label">What would you like to add to the memory bank?</div>

<textarea id="text" placeholder="Describe a useful pattern, a gotcha, a workflow tip, or anything worth sharing with your team…"></textarea>

      <div class="fields-row">
        <div class="field">
          <label>Type</label>
          <select id="type">${TYPES.map(t => `<option>${t}</option>`).join("")}</select>
        </div>
        <div class="field">
          <label>Scope</label>
          <select id="scope"><option>auto</option><option>Team</option><option>Personal</option></select>
        </div>
      </div>

      <label class="checkbox-row">
        <input type="checkbox" id="unsure">
        Not sure — park this in UNSURE.md for later
      </label>

      <div class="divider"></div>

      <div class="button-row">
        <button class="btn-primary" id="save">
          <svg viewBox="0 0 16 16"><path d="M14 2H9l-7 7 5 5 7-7V2z"/><circle cx="12" cy="4" r="1.2" fill="currentColor" stroke="none"/></svg>
          Save to memory
        </button>
        <button class="btn-outline" id="raw">
          <svg viewBox="0 0 16 16"><path d="M2 2h9l3 3v9H2V2z"/><rect x="4" y="9" width="8" height="5" rx="1"/><rect x="4" y="2" width="6" height="4" rx="1"/></svg>
          Save without classification
        </button>
      </div>

      <label class="attach-link">
        <svg viewBox="0 0 16 16"><path d="M13.5 7.5l-5.5 5.5a3.5 3.5 0 01-5-5l6-6a2 2 0 012.8 2.8L6 11a.7.7 0 01-1-1l5.5-5.5"/></svg>
        Attach a file
        <input id="file" type="file" accept=".txt,text/plain" style="display:none" />
      </label>

      <div id="preview"></div>
    </div>
  `;

  const $ = sel => root.querySelector(sel);

  const textArea = $("#text");

  $("#unsure").onchange = () => {
    if ($("#unsure").checked) $("#type").value = "Unsure";
  };

  $("#type").onchange = () => {
    if ($("#type").value !== "Unsure") $("#unsure").checked = false;
    else $("#unsure").checked = true;
  };

  $("#file").onchange = async (e) => {
    const f = e.target.files[0];
    if (!f) return;
    if (f.size > 1024 * 1024) { toast("File too large (max 1MB)", true); return; }
    $("#text").value = await f.text();
  };

  const SAVE_ICON = `<svg viewBox="0 0 16 16"><path d="M14 2H9l-7 7 5 5 7-7V2z"/><circle cx="12" cy="4" r="1.2" fill="currentColor" stroke="none"/></svg>`;
  const RAW_ICON  = `<svg viewBox="0 0 16 16"><path d="M2 2h9l3 3v9H2V2z"/><rect x="4" y="9" width="8" height="5" rx="1"/><rect x="4" y="2" width="6" height="4" rx="1"/></svg>`;

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

  // Primary: categorize then save
  $("#save").onclick = async () => {
    const saveBtn = $("#save");
    const rawBtn  = $("#raw");
    const text = $("#text").value.trim();
    if (!text) { toast("Type or attach some text first.", true); return; }
    saveBtn.disabled = true;
    rawBtn.disabled  = true;
    saveBtn.innerHTML = `${SAVE_ICON} Classifying…`;
    try {
      const backend = await pickBackend({ anthropicKey: config.anthropicKey });
      if (!backend) { toast("No LLM available — add an Anthropic key in Setup or ensure the binary is running.", true); return; }
      const indexFile = await getIndex(gh, cache);
      const scope = resolveScope($("#scope").value, config.owner);
      saveBtn.innerHTML = `${SAVE_ICON} Saving…`;
      const preset = await backend.categorize({
        index: indexFile.content,
        payload: { scope, type: $("#type").value, text, source: "UI", timestamp: new Date().toISOString() },
      });
      await commitEntry(preset);
    } catch (e) {
      toast("Save failed: " + e.message, true);
    } finally {
      saveBtn.innerHTML = `${SAVE_ICON} Save to memory`;
      saveBtn.disabled = false;
      rawBtn.disabled  = false;
    }
  };

  // Secondary: save raw without LLM
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
