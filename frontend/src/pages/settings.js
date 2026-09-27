import { api } from "../services/api.js";
import { escapeHtml } from "../services/html.js";
import { ago } from "../lib/view.js";

const MODE_ORDER = ["auto", "ai", "review-timeout", "review", "manual"];
const MODE_NAMES = {
  "auto": "Everything automatically",
  "ai": "AI decides",
  "review-timeout": "Review, publish after a few days",
  "review": "Review everything",
  "manual": "Manual only",
};

function modeOptions(s, selected) {
  const allowed = new Set(s?.allowed_modes ?? MODE_ORDER);
  const help = s?.mode_help ?? {};
  return MODE_ORDER.map(m => {
    const disabled = !allowed.has(m);
    const rec = m === "ai" ? ` <span class="pill">recommended</span>` : "";
    const team = s?.team_default === m ? ` <span class="pill muted-pill">team default</span>` : "";
    return `
      <label class="mode-option${disabled ? " disabled" : ""}">
        <input type="radio" name="mode" value="${m}" ${m === selected ? "checked" : ""} ${disabled ? "disabled" : ""}>
        <span><strong>${escapeHtml(MODE_NAMES[m])}</strong>${rec}${team}<br>
        <span class="muted">${escapeHtml(help[m] ?? "")}${disabled ? " — not allowed by your team's policy" : ""}</span></span>
      </label>`;
  }).join("");
}

export async function renderSettings(root, { settings: s, toast, go, refreshSettings }) {
  const configured = !!s?.configured;
  const selectedMode = s?.mode || (configured ? s.effective_mode : "ai");
  root.innerHTML = `
    <div class="card">
      <h2>${configured ? "Settings" : "Set up team memory"}</h2>
      ${configured ? `
        <p class="connected-line"><span class="footer-dot connected"></span>
          Connected to <a href="https://github.com/${escapeHtml(s.owner)}/${escapeHtml(s.repo)}" target="_blank" rel="noopener noreferrer">${escapeHtml(s.owner)}/${escapeHtml(s.repo)}</a>
          as <strong>${escapeHtml(s.username)}</strong>${s.token_source === "gh" ? " (GitHub CLI login)" : ""}</p>
        <p class="muted">Last synced ${escapeHtml(ago(s.last_sync) || "never")} · AI: ${escapeHtml(s.llm)}
          <button class="link" id="sync">Sync now</button></p>
      ` : `
        <p class="muted">Team memory lives in a GitHub repository your team owns — every memory is a Markdown file.
          Create an empty private repo if you don't have one yet.</p>
      `}
    </div>

    <div class="card">
      <div class="section-label">How should knowledge from your Claude Code sessions be shared?</div>
      <div id="modes" class="modes">${modeOptions(s, selectedMode)}</div>
      ${configured ? `<p class="muted small">Team policy: default <strong>${escapeHtml(s.team_default)}</strong>, minimum <strong>${escapeHtml(s.team_minimum)}</strong>
        (edit <code>.team-memory/policy.yml</code> in the memory repo). Things you save yourself are always published.</p>` : ""}
    </div>

    <div class="card">
      <div class="section-label">${configured ? "Change repository or token" : "Repository access"}</div>
      <label class="field-label" for="repo">Memory repository (owner/name)</label>
      <input id="repo" class="search-input" type="text" placeholder="acme/team-memory" autocomplete="off" value="${escapeHtml(configured ? `${s.owner}/${s.repo}` : "")}">
      <label class="checkbox-row"><input type="checkbox" id="use-gh" ${s?.token_source === "gh" ? "checked" : ""}>
        Use my GitHub CLI login (<code>gh auth login</code>) — nothing to paste or rotate</label>
      <div id="token-wrap">
        <label class="field-label" for="pat">GitHub token ${configured ? `<span class="muted">(leave blank to keep the current one)</span>` : ""}</label>
        <input id="pat" class="search-input" type="password" autocomplete="off" placeholder="github_pat_…">
        <p class="muted small">Fine-grained token with <strong>Contents: Read and write</strong> on the memory repo —
          <a href="https://github.com/settings/personal-access-tokens/new" target="_blank" rel="noopener noreferrer">create one</a>.
          It's stored in your local config file, never in the browser.</p>
      </div>
      <div class="button-row"><button class="btn-primary" id="save">${configured ? "Save" : "Connect"}</button></div>
      <div id="status" class="muted" role="status"></div>
    </div>
    ${configured ? `<p class="muted small center">Config: ${escapeHtml(s.config_path)} · Data: ${escapeHtml(s.data_dir)}</p>` : ""}
  `;
  const $ = sel => root.querySelector(sel);
  const syncTokenField = () => { $("#token-wrap").hidden = $("#use-gh").checked; };
  $("#use-gh").onchange = syncTokenField;
  syncTokenField();

  if (configured) {
    root.querySelectorAll('input[name="mode"]').forEach(r => r.addEventListener("change", async () => {
      try {
        const res = await api.setMode(r.value);
        if (res.warning) toast(res.warning, true); else toast(`Capture mode: ${MODE_NAMES[r.value]}`);
        await refreshSettings();
      } catch (e) { toast(e.message, true); }
    }));
    $("#sync").onclick = async () => {
      try {
        const r = await api.sync();
        toast(r.changed ? "Synced — new memories pulled." : "Already up to date.");
        await refreshSettings();
        go("settings");
      } catch (e) { toast(e.message, true); }
    };
  }

  $("#save").onclick = async () => {
    const status = $("#status");
    const repo = $("#repo").value.trim();
    const useGH = $("#use-gh").checked;
    const token = $("#pat").value.trim();
    const mode = root.querySelector('input[name="mode"]:checked')?.value ?? "";
    if (!/^[^/\s]+\/[^/\s]+$/.test(repo)) { status.textContent = "Enter the repository as owner/name."; return; }
    if (!useGH && !token && !configured) { status.textContent = "Paste a token, or use your GitHub CLI login."; return; }
    $("#save").disabled = true;
    status.textContent = "Checking access…";
    try {
      const res = await api.setup({ repo, token, use_gh: useGH, mode, keep_token: configured && !token && !useGH });
      status.textContent = `Connected to ${res.repo} as ${res.username} — ${res.memories} memories.`;
      toast("Connected.");
      await refreshSettings();
      go(configured ? "settings" : "remember");
    } catch (e) {
      status.textContent = e.message;
    } finally {
      $("#save").disabled = false;
    }
  };
}
