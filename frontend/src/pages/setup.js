import { GitHubClient } from "../services/github.js";

const SEED_INDEX = `# INDEX for team-memory
GENERAL.md | shared | general
UNSURE.md | shared | unsure
`;

export function renderSetup(root, { onDone, config }) {
  root.innerHTML = `
    <div class="card">
      <h2>Setup</h2>
      <label>GitHub Personal Access Token <span class="muted">(fine-grained, contents:write on one repo)</span></label>
      <input id="pat" type="password" value="${config.token ?? ""}" />
      <label>Repository (owner/name)</label>
      <input id="repo" type="text" placeholder="AndrewSkea/my-knowledge" value="${config.repo ?? ""}" />
      <label>Anthropic API key <span class="muted">(optional — uses local MCP if blank)</span></label>
      <input id="anthropic" type="password" value="${config.anthropicKey ?? ""}" />
      <p class="muted">Credentials are stored only in this browser's localStorage. Treat this profile as the trust boundary.</p>
      <button class="primary" id="save">Save & verify</button>
      <div id="status" class="muted"></div>
    </div>
  `;
  root.querySelector("#save").onclick = async () => {
    const status = root.querySelector("#status");
    const token = root.querySelector("#pat").value.trim();
    const repoStr = root.querySelector("#repo").value.trim();
    const anthropicKey = root.querySelector("#anthropic").value.trim();
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
      const next = { token, owner, repo, anthropicKey, username: user.login };
      onDone(next);
    } catch (e) {
      status.textContent = "Error: " + e.message;
    }
  };
}
