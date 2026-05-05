export class LocalMCP {
  constructor({ url = "http://127.0.0.1:7438", loadPrompt, fetch: f = globalThis.fetch }) {
    this.url = url;
    this.loadPrompt = loadPrompt;
    this.fetch = f;
  }

  async healthy() {
    try {
      const r = await this.fetch(`${this.url}/health`, { method: "GET" });
      return r.ok;
    } catch {
      return false;
    }
  }

  async categorize({ index, payload }) {
    const r = await this.fetch(`${this.url}/v1/categorize`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ index, payload, token_budget: 200 }),
    });
    if (!r.ok) throw new Error(`mcp ${r.status}: ${await r.text().catch(() => "")}`);
    const text = await r.text();
    try { return JSON.parse(text); } catch { throw new Error("mcp returned non-JSON: " + text.slice(0, 200)); }
  }

  async summarize({ filename, text }) {
    const r = await this.fetch(`${this.url}/v1/summarize`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ filename, text }),
    });
    if (!r.ok) throw new Error(`mcp ${r.status}: ${await r.text().catch(() => "")}`);
    const t = await r.text();
    try { return JSON.parse(t); } catch { throw new Error("mcp returned non-JSON: " + t.slice(0, 200)); }
  }
}
