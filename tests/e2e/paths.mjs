import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

export const PORT = 19438;
export const GH_PORT = 19439;
export const E2E_DIR = join(tmpdir(), "team-memory-e2e");
export const bin = name => resolve("mcp", process.platform === "win32" ? `${name}.exe` : name);
