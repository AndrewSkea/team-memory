import { AnthropicDirect } from "./anthropic.js";
import { LocalMCP } from "./mcp.js";

let _promptCache = {};
async function loadPrompt(name) {
  if (_promptCache[name]) return _promptCache[name];
  const r = await fetch(`./prompts/${name}.txt`);
  if (!r.ok) throw new Error(`failed to load prompt ${name}`);
  _promptCache[name] = await r.text();
  return _promptCache[name];
}

export async function pickBackend({ anthropicKey }) {
  if (anthropicKey) return new AnthropicDirect({ apiKey: anthropicKey, loadPrompt });
  const mcp = new LocalMCP({ loadPrompt });
  if (await mcp.healthy()) return mcp;
  return null;
}
