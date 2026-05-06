const ANTHROPIC_API = "https://api.anthropic.com/v1/messages";
const MODEL = "claude-sonnet-4-6";

export class AnthropicDirect {
  constructor({ apiKey, loadPrompt, fetch: f = globalThis.fetch }) {
    this.apiKey = apiKey;
    this.loadPrompt = loadPrompt;
    this.fetch = (...args) => f(...args);
  }

  async _call(systemPrompt, userContent) {
    const res = await this.fetch(ANTHROPIC_API, {
      method: "POST",
      headers: {
        "x-api-key": this.apiKey,
        "anthropic-version": "2023-06-01",
        "anthropic-dangerous-direct-browser-access": "true",
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        model: MODEL,
        max_tokens: 1024,
        system: systemPrompt,
        messages: [{ role: "user", content: userContent }],
      }),
    });
    if (!res.ok) {
      const t = await res.text().catch(() => "");
      throw new Error(`anthropic ${res.status}: ${t.slice(0, 200)}`);
    }
    const j = await res.json();
    const text = (j.content ?? []).filter(c => c.type === "text").map(c => c.text).join("");
    try { return JSON.parse(text); } catch { throw new Error("anthropic returned non-JSON: " + text.slice(0, 200)); }
  }

  async categorize({ index, payload }) {
    const sys = await this.loadPrompt("categorize");
    return this._call(sys, `INDEX:\n${index}\nPAYLOAD:\n${JSON.stringify(payload)}`);
  }

  async summarize({ filename, text }) {
    const sys = await this.loadPrompt("summarize");
    return this._call(sys, JSON.stringify({ filename, text }));
  }

  async healthy() { return true; }
}
