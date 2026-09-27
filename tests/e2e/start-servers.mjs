// Starts a fake GitHub and team-memory with an isolated config/data dir, for
// Playwright. Cross-platform replacement for a shell one-liner.
import { spawn } from "node:child_process";
import { rmSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { E2E_DIR, PORT, GH_PORT, bin } from "./paths.mjs";

rmSync(E2E_DIR, { recursive: true, force: true });
mkdirSync(E2E_DIR, { recursive: true });

const children = [];
const start = (cmd, args, env = {}) => {
  const c = spawn(cmd, args, { stdio: "inherit", env: { ...process.env, ...env } });
  c.on("exit", code => { if (code) { console.error(`${cmd} exited ${code}`); process.exit(code); } });
  children.push(c);
};
const stop = () => { for (const c of children) c.kill(); process.exit(0); };
process.on("SIGINT", stop);
process.on("SIGTERM", stop);

start(bin("fakegh"), ["-addr", `127.0.0.1:${GH_PORT}`]);
start(bin("team-memory-mcp"), ["serve", "--port", String(PORT)], {
  TEAM_MEMORY_API_URL: `http://127.0.0.1:${GH_PORT}`,
  TEAM_MEMORY_CONFIG: join(E2E_DIR, "config.json"),
  TEAM_MEMORY_DATA: join(E2E_DIR, "data"),
  TEAM_MEMORY_CLAUDE: join(E2E_DIR, "no-claude-here"),
  ANTHROPIC_API_KEY: "",
  TEAM_MEMORY_NO_SPAWN: "1",
});
