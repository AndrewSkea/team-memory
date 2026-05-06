import { GitHubClient } from "../services/github.js";
import { Cache } from "../services/cache.js";
import { parseIndex, serializeIndex, upsertEntry } from "../services/indexmd.js";
import { renderEntry } from "../services/entries.js";
import { pickBackend } from "../services/llm/backend.js";

const TYPES = ["General", "Meeting Notes", "Unsure", "Programming", "Ideas", "Reminders"];

export function renderRemember(root, { config, toast }) {
  const gh = new GitHubClient({ token: config.token, owner: config.owner, repo: config.repo });
  const cache = new Cache("team-memory");

  root.innerHTML = `
    <div class="card">
      <h2>Remember</h2>
      <label>Memory text</label>
      <textarea id="text" placeholder="What do you want to remember?"></textarea>
      <label>Or upload a .txt file</label>
      <input id="file" type="file" accept=".txt,text/plain" />
      <label>Scope</label>
      <select id="scope"><option>Team</option><option>Personal</option></select>
      <label>Type</label>
      <select id="type">${TYPES.map(t => `<option>${t}</option>`).join("")}</select>
      <label>Target file <span class="muted">(leave blank to auto-pick or fall back to GENERAL.md / UNSURE.md)</span></label>
      <input id="target" type="text" placeholder="shared/programming-practices.md" />
      <div style="display:flex; gap:8px; margin-top:12px;">
        <button class="primary" id="auto">Auto-categorize</button>
        <button class="primary" id="save">Save</button>
      </div>
      <div id="preview"></div>
    </div>
  `;

  const $ = sel => root.querySelector(sel);

  $("#file").onchange = async (e) => {
    const f = e.target.files[0];
    if (!f) return;
    if (f.size > 1024 * 1024) { toast("File too large (max 1MB for slice 1)", true); return; }
    $("#text").value = await f.text();
  };

  $("#auto").onclick = async () => {
    const btn = $("#auto");
    const saveBtn = $("#save");
    try {
      const text = $("#text").value.trim();
      if (!text) { toast("Type or upload some text first.", true); return; }
      btn.textContent = "Categorizing…";
      btn.disabled = true;
      saveBtn.disabled = true;
      const backend = await pickBackend({ anthropicKey: config.anthropicKey, mcpUrl: "http://127.0.0.1:7438" });
      if (!backend) { toast("No LLM available. Configure an Anthropic key or start MCP.", true); return; }
      const indexFile = await getIndex(gh, cache);
      const scope = $("#scope").value === "Team" ? "Team" : `Personal:${config.username}`;
      const result = await backend.categorize({
        index: indexFile.content,
        payload: {
          scope, type: $("#type").value, text,
          source: "UI", timestamp: new Date().toISOString(),
        },
      });
      $("#target").value = result.target_file;
      $("#preview").innerHTML = `<div class="result"><b>${result.short_title}</b><br><span class="muted">${result.one_sentence_summary}</span><br>tags: ${result.tags}${result.unsure ? '<br><small style="color:var(--danger)">low confidence — will save to UNSURE.md</small>' : ""}<br><br><b style="color:var(--accent)">↓ Click Save to commit to GitHub</b></div>`;
      $("#save").dataset.preset = JSON.stringify(result);
    } catch (e) {
      toast("Auto-categorize failed: " + e.message, true);
    } finally {
      btn.textContent = "Auto-categorize";
      btn.disabled = false;
      saveBtn.disabled = false;
    }
  };

  $("#save").onclick = async () => {
    const saveBtn = $("#save");
    const autoBtn = $("#auto");
    saveBtn.textContent = "Saving…";
    saveBtn.disabled = true;
    autoBtn.disabled = true;

    const preset = $("#save").dataset.preset ? JSON.parse($("#save").dataset.preset) : null;

    async function doSave() {
      try {
        const text = $("#text").value.trim();
        if (!text) { toast("Empty memory.", true); return; }
        const type = $("#type").value;
        const scopeSel = $("#scope").value;
        const scope = scopeSel === "Team" ? "Team" : `Personal:${config.username}`;
        const target = $("#target").value.trim() || (type === "Unsure" ? "UNSURE.md" : "GENERAL.md");
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
          if (res.kind === "conflict") {
            toast("Could not save — file changed in GitHub. Open it there and try again.", true);
          } else if (res.kind === "auth") {
            toast("Auth failed — check your PAT.", true);
          } else {
            toast(`Save failed (${res.kind}): ${res.message ?? ""}`, true);
          }
          return;
        }
        const idxFile = await getIndex(gh, cache, true);
        const idx = parseIndex(idxFile.content);
        if (!idx.entries.find(e => e.path === target)) {
          const scopeStr = target.startsWith("users/") ? `personal:${config.username}` : "shared";
          const topics = preset?.tags ?? type.toLowerCase();
          upsertEntry(idx, { path: target, scope: scopeStr, topics });
          await gh.putContent({ path: "INDEX.md", content: serializeIndex(idx), message: "team-memory: update INDEX.md" });
        }
        await cache.delete("file:" + target);
        await cache.delete("file:INDEX.md");
        $("#text").value = "";
        $("#preview").innerHTML = "";
        $("#save").dataset.preset = "";
        toast(`Saved to ${target}.`);
      } catch (e) {
        toast("Save failed: " + e.message, true);
      } finally {
        saveBtn.textContent = "Save";
        saveBtn.disabled = false;
        autoBtn.disabled = false;
      }
    }

    if (config.check_first && !preset) {
      const existing = root.querySelector("#check-first-confirm");
      if (!existing) {
        const card = document.createElement("div");
        card.id = "check-first-confirm";
        card.className = "result";
        card.innerHTML = `Save this text to GENERAL.md as an unstructured entry? <button id="cfyes" class="primary" style="margin-left:8px;">Save</button> <button id="cfno" style="margin-left:4px;">Cancel</button>`;
        const preview = $("#preview");
        preview.parentNode.insertBefore(card, preview);
        card.querySelector("#cfno").onclick = () => {
          card.remove();
          saveBtn.textContent = "Save";
          saveBtn.disabled = false;
          autoBtn.disabled = false;
        };
        card.querySelector("#cfyes").onclick = () => {
          card.remove();
          doSave();
        };
      } else {
        saveBtn.textContent = "Save";
        saveBtn.disabled = false;
        autoBtn.disabled = false;
      }
      return;
    }

    await doSave();
  };
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
