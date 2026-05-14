import { GitHubClient } from "../services/github.js";
import { escapeHtml } from "../services/html.js";

const SEED_INDEX = `# INDEX for team-memory
GENERAL.md | shared | general
UNSURE.md | shared | unsure
`;

export function renderSetup(root, { onDone, config }) {
  const checkFirst = config.check_first ?? false;
  root.innerHTML = `
    <div class="card">
      <h2>Setup</h2>
      <label>GitHub Personal Access Token <span class="muted">(fine-grained, contents:write on one repo)</span></label>
      <input id="pat" type="password" value="${escapeHtml(config.token ?? "")}" />
      <label>Repository (owner/name)</label>
      <input id="repo" type="text" placeholder="AndrewSkea/my-knowledge" value="${escapeHtml(config.repo ?? "")}" />
      <label>Anthropic API key <span class="muted">(optional — uses local MCP if blank)</span></label>
      <input id="anthropic" type="password" value="${escapeHtml(config.anthropicKey ?? "")}" />
      <p class="muted">Credentials are stored only in this browser's localStorage. Treat this profile as the trust boundary.</p>
      <button class="primary" id="save">Save & verify</button>
      <div id="status" class="muted"></div>
      <div id="cli-section" style="display:none; margin-top:16px; border-top:1px solid var(--border); padding-top:16px;">
        <label style="display:flex; align-items:center; gap:8px; cursor:pointer;">
          <input type="checkbox" id="check-first" ${checkFirst ? "checked" : ""} />
          Check first before saving <span class="muted">(browser Remember page only)</span>
        </label>
        <button id="export-cfg" style="margin-top:12px;">Export to CLI config</button>
        <div id="export-status" class="muted" style="margin-top:4px;"></div>
        <p class="muted" style="margin-top:12px;">Then add to <code>~/.claude/settings.json</code>:</p>
        <pre style="background:var(--bg);padding:8px;border-radius:4px;font-size:12px;overflow-x:auto;">{
  "hooks": {
    "Stop": [{"matcher":"","hooks":[{"type":"command","command":"team-memory-mcp --once session-end"}]}],
    "PreCompact": [{"matcher":"","hooks":[{"type":"command","command":"team-memory-mcp --once precompact"}]}]
  }
}</pre>
      </div>
    </div>
  `;

  const $ = sel => root.querySelector(sel);

  $("#save").onclick = async () => {
    const status = $("#status");
    const token = $("#pat").value.trim();
    const repoStr = $("#repo").value.trim();
    const anthropicKey = $("#anthropic").value.trim();
    if (!token || !repoStr.includes("/")) {
      status.textContent = "Token and owner/repo are required.";
      return;
    }
    const [owner, repo] = repoStr.split("/");
    const gh = new GitHubClient({ token, owner, repo });
    try {
      status.textContent = "Verifying token…";
      const user = await gh.getUser();
      status.textContent = `Authenticated as ${user.login}. Checking repo…`;
      const idx = await gh.getFile("INDEX.md");
      if (!idx.exists) {
        await gh.putContent({ path: "INDEX.md", content: SEED_INDEX, message: "team-memory: seed INDEX.md" });
        await gh.putContent({ path: "GENERAL.md", content: "# GENERAL\n", message: "team-memory: seed GENERAL.md" });
        await gh.putContent({ path: "UNSURE.md", content: "# UNSURE\n", message: "team-memory: seed UNSURE.md" });
        status.textContent = "Seeded fresh repo.";
      }
      const checkFirst = $("#check-first")?.checked ?? false;
      const next = { token, owner, repo, anthropicKey, username: user.login, check_first: checkFirst };
      onDone(next);
      $("#cli-section").style.display = "";
    } catch (e) {
      status.textContent = "Error: " + e.message;
    }
  };

  if (config.token && config.owner && config.repo) {
    $("#cli-section").style.display = "";
  }

  $("#check-first").onchange = () => {
    const stored = JSON.parse(localStorage.getItem("team-memory-config") ?? "{}");
    stored.check_first = $("#check-first").checked;
    localStorage.setItem("team-memory-config", JSON.stringify(stored));
  };

  $("#export-cfg").onclick = async () => {
    const status = $("#export-status");
    const token = $("#pat").value.trim();
    const repoStr = $("#repo").value.trim();
    if (!token || !repoStr.includes("/")) {
      status.textContent = "Save & verify first.";
      return;
    }
    const [owner, repo] = repoStr.split("/");
    const checkFirst = $("#check-first").checked;
    try {
      status.textContent = "Exporting…";
      const res = await fetch("/v1/export-config", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token, owner, repo, check_first: checkFirst }),
      });
      if (!res.ok) {
        const j = await res.json().catch(() => ({}));
        status.textContent = "✗ " + (j.error ?? `HTTP ${res.status}`);
        return;
      }
      const j = await res.json();
      status.textContent = "✓ Config saved to " + j.path;
    } catch {
      status.textContent = "✗ MCP not running — start it first";
    }
  };
}
