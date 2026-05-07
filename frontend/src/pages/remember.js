import { GitHubClient } from "../services/github.js";
import { Cache } from "../services/cache.js";
import { parseIndex, serializeIndex, upsertEntry } from "../services/indexmd.js";
import { renderEntry } from "../services/entries.js";
import { pickBackend } from "../services/llm/backend.js";

const TYPES = ["General", "Meeting Notes", "Programming", "Ideas", "Reminders", "Unsure"];

export function renderRemember(root, { config, toast, forgetAuth }) {
  const gh = new GitHubClient({ token: config.token, owner: config.owner, repo: config.repo });
  const cache = new Cache("team-memory");

  const repoDisplay = config.owner && config.repo ? `${config.owner}/${config.repo}` : "not configured";

  root.innerHTML = `
    <div class="card">
      <div class="user-row">
        <div class="user-dot"></div>
        <div class="user-details">
          <span class="user-name">${config.owner || "—"}</span>
          <span class="user-repo">${repoDisplay}</span>
        </div>
        <button id="forget-btn" title="Forget auth" style="background:none;border:none;cursor:pointer;margin-left:auto;padding:4px;color:var(--muted);font-size:12px;">✕</button>
      </div>

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

      <div class="button-row">
        <button class="btn-primary" id="save">
          <svg viewBox="0 0 16 16"><path d="M2 2h9l3 3v9H2V2z"/><rect x="4" y="9" width="8" height="5" rx="1"/><rect x="4" y="2" width="6" height="4" rx="1"/></svg>
          Save to memory
        </button>
        <button class="btn-outline" id="auto">
          <svg viewBox="0 0 16 16"><path d="M14 2H9l-7 7 5 5 7-7V2z"/><circle cx="12" cy="4" r="1.2" fill="currentColor" stroke="none"/></svg>
          Auto-tag
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

  $("#forget-btn").onclick = forgetAuth;

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

  $("#auto").onclick = async () => {
    const btn = $("#auto");
    const saveBtn = $("#save");
    try {
      const text = $("#text").value.trim();
      if (!text) { toast("Type or attach some text first.", true); return; }
      btn.textContent = "Tagging…";
      btn.disabled = true;
      saveBtn.disabled = true;
      const backend = await pickBackend({ anthropicKey: config.anthropicKey });
      if (!backend) { toast("No LLM available — configure an Anthropic key or start MCP.", true); return; }
      const indexFile = await getIndex(gh, cache);
      const scope = resolveScope($("#scope").value, config.owner);
      const result = await backend.categorize({
        index: indexFile.content,
        payload: { scope, type: $("#type").value, text, source: "UI", timestamp: new Date().toISOString() },
      });
      $("#save").dataset.preset = JSON.stringify(result);
      const unsure = result.unsure;
      $("#preview").innerHTML = `
        <div class="preview-block">
          <div class="preview-title">${result.short_title}</div>
          <div class="preview-summary">${result.one_sentence_summary}</div>
          <div class="muted" style="font-size:12px;margin-bottom:6px;">tags: ${result.tags}${unsure ? ' · <span style="color:var(--danger)">low confidence → UNSURE.md</span>' : ""}</div>
          <div class="preview-hint">↓ Click Save to memory to commit</div>
        </div>`;
    } catch (e) {
      toast("Auto-tag failed: " + e.message, true);
    } finally {
      btn.innerHTML = `<svg viewBox="0 0 16 16"><path d="M14 2H9l-7 7 5 5 7-7V2z"/><circle cx="12" cy="4" r="1.2" fill="currentColor" stroke="none"/></svg> Auto-tag`;
      btn.disabled = false;
      saveBtn.disabled = false;
    }
  };

  $("#save").onclick = async () => {
    const saveBtn = $("#save");
    const autoBtn = $("#auto");
    saveBtn.disabled = true;
    autoBtn.disabled = true;

    const preset = saveBtn.dataset.preset ? JSON.parse(saveBtn.dataset.preset) : null;

    async function doSave() {
      try {
        const text = $("#text").value.trim();
        if (!text) { toast("Empty memory.", true); return; }
        const type = $("#type").value;
        const scope = resolveScope($("#scope").value, config.owner);
        const useUnsure = $("#unsure").checked || type === "Unsure";
        const target = useUnsure ? "UNSURE.md" : "GENERAL.md";
        const timestamp = new Date().toISOString();
        const entry = {
          timestamp,
          shortTitle: preset?.short_title ?? text.split("\n")[0].slice(0, 60),
          scope, type,
          tags: preset?.tags ?? "",
          source: "UI",
          summary: preset?.one_sentence_summary ?? "",
          bullets: preset?.bullets ?? [],
          full: text,
        };
        const block = "\n" + renderEntry(entry);
        const res = await gh.commitFile({ path: target, append: block, message: `team-memory: add entry to ${target}` });
        if (!res.ok) {
          if (res.kind === "conflict") toast("File changed on GitHub — try again.", true);
          else if (res.kind === "auth") toast("Auth failed — check your PAT.", true);
          else toast(`Save failed (${res.kind}): ${res.message ?? ""}`, true);
          return;
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
        saveBtn.dataset.preset = "";
        toast(`Saved to ${target}.`);
      } catch (e) {
        toast("Save failed: " + e.message, true);
      } finally {
        saveBtn.innerHTML = `<svg viewBox="0 0 16 16"><path d="M2 2h9l3 3v9H2V2z"/><rect x="4" y="9" width="8" height="5" rx="1"/><rect x="4" y="2" width="6" height="4" rx="1"/></svg> Save to memory`;
        saveBtn.disabled = false;
        autoBtn.disabled = false;
      }
    }

    if (config.check_first && !preset) {
      if (!root.querySelector("#check-first-confirm")) {
        const box = document.createElement("div");
        box.id = "check-first-confirm";
        box.className = "confirm-box";
        box.innerHTML = `Save as unstructured entry to GENERAL.md?
          <button id="cfyes" class="btn-primary" style="padding:6px 14px;font-size:13px;">Save</button>
          <button id="cfno" class="btn-outline" style="padding:6px 14px;font-size:13px;">Cancel</button>`;
        $("#preview").before(box);
        box.querySelector("#cfno").onclick = () => {
          box.remove();
          saveBtn.innerHTML = `<svg viewBox="0 0 16 16"><path d="M2 2h9l3 3v9H2V2z"/><rect x="4" y="9" width="8" height="5" rx="1"/><rect x="4" y="2" width="6" height="4" rx="1"/></svg> Save to memory`;
          saveBtn.disabled = false;
          autoBtn.disabled = false;
        };
        box.querySelector("#cfyes").onclick = () => { box.remove(); doSave(); };
      } else {
        saveBtn.innerHTML = `<svg viewBox="0 0 16 16"><path d="M2 2h9l3 3v9H2V2z"/><rect x="4" y="9" width="8" height="5" rx="1"/><rect x="4" y="2" width="6" height="4" rx="1"/></svg> Save to memory`;
        saveBtn.disabled = false;
        autoBtn.disabled = false;
      }
      return;
    }
    await doSave();
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
