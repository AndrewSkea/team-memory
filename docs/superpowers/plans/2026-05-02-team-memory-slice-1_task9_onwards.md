# team-memory Slice 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship slice 1 of team-memory: a static web app + minimal Go MCP that lets a single user save and look up Markdown memories in their GitHub repo, optionally with LLM-driven categorization (browser Anthropic key OR local MCP shelling out to `claude -p`).

**Architecture:** Vanilla-JS static frontend (no framework) + Go MCP binary that wraps `claude -p --output-format stream-json --verbose`. Frontend talks directly to GitHub REST and (optionally) directly to Anthropic; MCP is a loopback-only HTTP service that does nothing but call the LLM. Prompts live once at repo root and are copied into the frontend bundle and embedded into the Go binary.

**Tech Stack:** HTML/CSS/ES modules, `node --test` (zero deps) for frontend tests, `fake-indexeddb` for IndexedDB tests, Go 1.22+, `go test`, `httptest`. No frameworks, no bundlers (slice 1 uses native ES modules served as files).

**Build order rationale:** prompts/bootstrap → MCP (so the LLM path is testable via `curl` before frontend exists) → frontend services bottom-up (TDD-friendly) → frontend pages → app shell → README. This means at every checkpoint there is something runnable and testable.

---

## File map

**Created:**

```
.gitignore
README.md
package.json                                    # npm test + copy-prompts
Makefile                                        # mcp build helper
prompts/categorize.txt
prompts/summarize.txt
mcp/go.mod
mcp/main.go
mcp/server/server.go
mcp/server/handlers.go
mcp/server/handlers_test.go
mcp/llm/claude.go
mcp/llm/claude_test.go
mcp/llm/stream.go
mcp/llm/stream_test.go
mcp/llm/testdata/stream_categorize.jsonl
mcp/llm/testdata/fake_claude.sh
mcp/llm/testdata/fake_claude.cmd
mcp/prompts/embed.go
mcp/prompts/categorize.txt                      # copied from /prompts at build
mcp/prompts/summarize.txt
frontend/index.html
frontend/src/app.js
frontend/src/pages/setup.js
frontend/src/pages/remember.js
frontend/src/pages/lookup.js
frontend/src/services/github.js
frontend/src/services/cache.js
frontend/src/services/indexmd.js
frontend/src/services/entries.js
frontend/src/services/llm/backend.js
frontend/src/services/llm/anthropic.js
frontend/src/services/llm/mcp.js
frontend/src/ui/main.css
frontend/prompts/categorize.txt                 # copied from /prompts at build
frontend/prompts/summarize.txt
tests/frontend/indexmd.test.js
tests/frontend/entries.test.js
tests/frontend/github.test.js
tests/frontend/cache.test.js
scripts/copy-prompts.sh
```

Each file has one responsibility. Frontend services are pure functions where possible (parse, render, classify) and isolate side effects (`fetch`, IndexedDB) into thin wrappers so tests can mock them.

---

## Task 9: Frontend — `entries.js` template renderer

**Files:**
- Create: `frontend/src/services/entries.js`
- Create: `tests/frontend/entries.test.js`

- [ ] **Step 1: Write the failing test**

`tests/frontend/entries.test.js`:

```js
import { test } from "node:test";
import assert from "node:assert/strict";
import { renderEntry, appendEntry } from "../../frontend/src/services/entries.js";

const ENTRY = {
  timestamp: "2026-05-02T09:48:00Z",
  shortTitle: "Enforce semicolon linting",
  scope: "Team",
  type: "Programming",
  tags: "eslint;linting;style",
  source: "UI",
  summary: "Proposal to adopt semicolon linting across repos.",
  bullets: ["Add ESLint rule semi:true", "Document in onboarding", "Run autofix in CI"],
  full: "We should adopt a linting rule...",
};

test("renderEntry produces expected markdown shape", () => {
  const md = renderEntry(ENTRY);
  assert.match(md, /^### Entry: 2026-05-02T09:48:00Z — Enforce semicolon linting$/m);
  assert.match(md, /\*\*Scope:\*\* Team/);
  assert.match(md, /\*\*Tags:\*\* eslint;linting;style/);
  assert.match(md, /Add ESLint rule semi:true/);
  assert.match(md, /We should adopt a linting rule/);
});

test("appendEntry preserves existing content with a separator", () => {
  const existing = "# Programming Practices\n\nSome intro.\n";
  const out = appendEntry(existing, ENTRY);
  assert.ok(out.startsWith("# Programming Practices"));
  assert.match(out, /### Entry: 2026-05-02T09:48:00Z/);
});

test("appendEntry handles empty existing content", () => {
  const out = appendEntry("", ENTRY);
  assert.match(out, /### Entry: 2026-05-02T09:48:00Z/);
});
```

- [ ] **Step 2: Run — fail**

```bash
npm test
```

- [ ] **Step 3: Implement `entries.js`**

```js
export function renderEntry(e) {
  const bullets = (e.bullets ?? []).map(b => `- ${b}`).join(" ; ");
  return [
    `### Entry: ${e.timestamp} — ${e.shortTitle}`,
    `**Scope:** ${e.scope}`,
    `**Type:** ${e.type}`,
    `**Tags:** ${e.tags ?? ""}`,
    `**Source:** ${e.source}`,
    `**Summary:** ${e.summary ?? ""}`,
    `**Bullets:** ${bullets}`,
    `**Full:**`,
    e.full ?? "",
    "",
  ].join("\n");
}

export function appendEntry(existing, entry) {
  const block = renderEntry(entry);
  if (!existing || !existing.trim()) return block;
  const sep = existing.endsWith("\n") ? "\n" : "\n\n";
  return existing + sep + block;
}
```

- [ ] **Step 4: Run — pass**

```bash
npm test
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/services/entries.js tests/frontend/entries.test.js
git commit -m "feat(frontend): entry template renderer + append helper"
```

---

## Task 10: Frontend — `github.js` SHA-retry commit

**Files:**
- Create: `frontend/src/services/github.js`
- Create: `tests/frontend/github.test.js`

The GitHub Contents API uses base64 for the `content` field. We `btoa(unescape(encodeURIComponent(text)))` for UTF-8 safety, and `decodeURIComponent(escape(atob(b64)))` for decode.

- [ ] **Step 1: Write the failing test**

`tests/frontend/github.test.js`:

```js
import { test } from "node:test";
import assert from "node:assert/strict";
import { GitHubClient } from "../../frontend/src/services/github.js";

function mockFetch(handlers) {
  const calls = [];
  const fn = async (url, opts = {}) => {
    calls.push({ url, opts });
    const handler = handlers.shift();
    if (!handler) throw new Error("unexpected fetch: " + url);
    return handler({ url, opts });
  };
  fn.calls = calls;
  return fn;
}

function jsonResponse(status, body, headers = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
}

const b64 = s => Buffer.from(s, "utf8").toString("base64");

test("commitFile creates a new file on 404", async () => {
  const fetch = mockFetch([
    () => jsonResponse(404, { message: "Not Found" }),
    () => jsonResponse(201, { content: { sha: "newsha" } }),
  ]);
  const gh = new GitHubClient({ token: "t", owner: "o", repo: "r", fetch });
  const res = await gh.commitFile({ path: "GENERAL.md", append: "hello", message: "msg" });
  assert.equal(res.ok, true);
  assert.equal(fetch.calls.length, 2);
  assert.equal(fetch.calls[1].opts.method, "PUT");
});

test("commitFile retries once on 409 then succeeds", async () => {
  const fetch = mockFetch([
    () => jsonResponse(200, { sha: "s1", content: b64("first\n"), encoding: "base64" }),
    () => jsonResponse(409, { message: "conflict" }),
    () => jsonResponse(200, { sha: "s2", content: b64("first\nsecond\n"), encoding: "base64" }),
    () => jsonResponse(200, { content: { sha: "s3" } }),
  ]);
  const gh = new GitHubClient({ token: "t", owner: "o", repo: "r", fetch });
  const res = await gh.commitFile({ path: "GENERAL.md", append: "third\n", message: "msg" });
  assert.equal(res.ok, true);
  assert.equal(fetch.calls.length, 4);
});

test("commitFile gives up after 3 retries", async () => {
  const fetch = mockFetch([
    () => jsonResponse(200, { sha: "s1", content: b64("a"), encoding: "base64" }),
    () => jsonResponse(409, { message: "c" }),
    () => jsonResponse(200, { sha: "s2", content: b64("a"), encoding: "base64" }),
    () => jsonResponse(409, { message: "c" }),
    () => jsonResponse(200, { sha: "s3", content: b64("a"), encoding: "base64" }),
    () => jsonResponse(409, { message: "c" }),
  ]);
  const gh = new GitHubClient({ token: "t", owner: "o", repo: "r", fetch });
  const res = await gh.commitFile({ path: "GENERAL.md", append: "x", message: "msg" });
  assert.equal(res.ok, false);
  assert.equal(res.kind, "conflict");
});

test("getUser returns login", async () => {
  const fetch = mockFetch([() => jsonResponse(200, { login: "andrew" })]);
  const gh = new GitHubClient({ token: "t", owner: "o", repo: "r", fetch });
  const u = await gh.getUser();
  assert.equal(u.login, "andrew");
});
```

- [ ] **Step 2: Run — fail**

```bash
npm test
```

- [ ] **Step 3: Implement `github.js`**

```js
const API = "https://api.github.com";

function b64encode(str) {
  if (typeof Buffer !== "undefined") return Buffer.from(str, "utf8").toString("base64");
  return btoa(unescape(encodeURIComponent(str)));
}
function b64decode(b64) {
  if (typeof Buffer !== "undefined") return Buffer.from(b64, "base64").toString("utf8");
  return decodeURIComponent(escape(atob(b64)));
}

export class GitHubClient {
  constructor({ token, owner, repo, fetch: f = globalThis.fetch }) {
    this.token = token;
    this.owner = owner;
    this.repo = repo;
    this.fetch = f;
  }

  async _req(path, opts = {}) {
    const res = await this.fetch(`${API}${path}`, {
      ...opts,
      headers: {
        Authorization: `Bearer ${this.token}`,
        Accept: "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
        ...(opts.headers ?? {}),
      },
    });
    return res;
  }

  async getUser() {
    const r = await this._req("/user");
    if (!r.ok) throw new Error(`getUser: ${r.status}`);
    return r.json();
  }

  async getFile(path) {
    const r = await this._req(`/repos/${this.owner}/${this.repo}/contents/${encodeURIComponent(path)}`);
    if (r.status === 404) return { exists: false, sha: null, content: "" };
    if (!r.ok) throw new Error(`getFile ${path}: ${r.status}`);
    const j = await r.json();
    return { exists: true, sha: j.sha, content: b64decode(j.content) };
  }

  async putFile({ path, content, sha, message }) {
    const body = { message, content: b64encode(content) };
    if (sha) body.sha = sha;
    const r = await this._req(`/repos/${this.owner}/${this.repo}/contents/${encodeURIComponent(path)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    return r;
  }

  // commitFile: read-modify-write with SHA-retry up to 3 attempts.
  // `append` is appended to existing content. For full replacement use putFile directly.
  async commitFile({ path, append, message }) {
    for (let attempt = 0; attempt < 3; attempt++) {
      const cur = await this.getFile(path);
      const next = (cur.content ?? "") + append;
      const r = await this.putFile({ path, content: next, sha: cur.sha, message });
      if (r.ok) return { ok: true };
      if (r.status === 409) continue;
      const body = await r.json().catch(() => ({}));
      return { ok: false, kind: classifyStatus(r.status), status: r.status, message: body.message };
    }
    return { ok: false, kind: "conflict", message: "exceeded 3 retries" };
  }

  // putContent: full file replacement (used for INDEX.md updates that aren't pure appends).
  async putContent({ path, content, message }) {
    for (let attempt = 0; attempt < 3; attempt++) {
      const cur = await this.getFile(path);
      const r = await this.putFile({ path, content, sha: cur.sha, message });
      if (r.ok) return { ok: true };
      if (r.status === 409) continue;
      const body = await r.json().catch(() => ({}));
      return { ok: false, kind: classifyStatus(r.status), status: r.status, message: body.message };
    }
    return { ok: false, kind: "conflict", message: "exceeded 3 retries" };
  }
}

function classifyStatus(s) {
  if (s === 401 || s === 403) return "auth";
  if (s === 404) return "not-found";
  if (s === 409) return "conflict";
  if (s === 422) return "invalid";
  return "other";
}
```

- [ ] **Step 4: Run — pass**

```bash
npm test
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/services/github.js tests/frontend/github.test.js
git commit -m "feat(frontend): GitHub REST client with SHA-retry commit"
```

---

## Task 11: Frontend — `cache.js` IndexedDB wrapper

**Files:**
- Create: `frontend/src/services/cache.js`
- Create: `tests/frontend/cache.test.js`

- [ ] **Step 1: Install `fake-indexeddb`**

```bash
npm install
```

(Pulls `fake-indexeddb` declared in Task 1.)

- [ ] **Step 2: Write the failing test**

`tests/frontend/cache.test.js`:

```js
import "fake-indexeddb/auto";
import { test } from "node:test";
import assert from "node:assert/strict";
import { Cache } from "../../frontend/src/services/cache.js";

test("set then get returns the value", async () => {
  const c = new Cache("test1");
  await c.set("k", { hello: "world" });
  const got = await c.get("k");
  assert.deepEqual(got, { hello: "world" });
});

test("get missing key returns undefined", async () => {
  const c = new Cache("test2");
  assert.equal(await c.get("missing"), undefined);
});

test("delete removes the key", async () => {
  const c = new Cache("test3");
  await c.set("k", 1);
  await c.delete("k");
  assert.equal(await c.get("k"), undefined);
});

test("clear empties the store", async () => {
  const c = new Cache("test4");
  await c.set("a", 1);
  await c.set("b", 2);
  await c.clear();
  assert.equal(await c.get("a"), undefined);
  assert.equal(await c.get("b"), undefined);
});
```

- [ ] **Step 3: Run — fail**

```bash
npm test
```

- [ ] **Step 4: Implement `cache.js`**

```js
const STORE = "kv";

function openDB(name) {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(name, 1);
    req.onupgradeneeded = () => req.result.createObjectStore(STORE);
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

function tx(db, mode) {
  return db.transaction(STORE, mode).objectStore(STORE);
}

function promisifyReq(req) {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

export class Cache {
  constructor(dbName = "team-memory") {
    this.dbName = dbName;
    this._db = null;
  }
  async _open() {
    if (!this._db) this._db = await openDB(this.dbName);
    return this._db;
  }
  async get(key) {
    const db = await this._open();
    return promisifyReq(tx(db, "readonly").get(key));
  }
  async set(key, value) {
    const db = await this._open();
    return promisifyReq(tx(db, "readwrite").put(value, key));
  }
  async delete(key) {
    const db = await this._open();
    return promisifyReq(tx(db, "readwrite").delete(key));
  }
  async clear() {
    const db = await this._open();
    return promisifyReq(tx(db, "readwrite").clear());
  }
}
```

- [ ] **Step 5: Run — pass**

```bash
npm test
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/services/cache.js tests/frontend/cache.test.js package-lock.json
git commit -m "feat(frontend): IndexedDB cache wrapper with fake-indexeddb tests"
```

---

## Task 12: Frontend — LLM backends

**Files:**
- Create: `frontend/src/services/llm/backend.js`
- Create: `frontend/src/services/llm/anthropic.js`
- Create: `frontend/src/services/llm/mcp.js`

These are not unit-tested in slice 1 (real network calls; we cover them via the manual smoke check). Code is small and dispatch logic is the only thing worth testing later.

- [ ] **Step 1: Create `backend.js` (interface + factory)**

```js
import { AnthropicDirect } from "./anthropic.js";
import { LocalMCP } from "./mcp.js";

// Loads the categorize prompt (bundled with the static site at /prompts/).
let _promptCache = {};
async function loadPrompt(name) {
  if (_promptCache[name]) return _promptCache[name];
  const r = await fetch(`./prompts/${name}.txt`);
  if (!r.ok) throw new Error(`failed to load prompt ${name}`);
  _promptCache[name] = await r.text();
  return _promptCache[name];
}

// Picks a backend at runtime. Returns null if neither is available.
export async function pickBackend({ anthropicKey, mcpUrl }) {
  if (anthropicKey) return new AnthropicDirect({ apiKey: anthropicKey, loadPrompt });
  const mcp = new LocalMCP({ url: mcpUrl, loadPrompt });
  if (await mcp.healthy()) return mcp;
  return null;
}
```

- [ ] **Step 2: Create `anthropic.js`**

```js
const ANTHROPIC_API = "https://api.anthropic.com/v1/messages";
const MODEL = "claude-sonnet-4-6";

export class AnthropicDirect {
  constructor({ apiKey, loadPrompt, fetch: f = globalThis.fetch }) {
    this.apiKey = apiKey;
    this.loadPrompt = loadPrompt;
    this.fetch = f;
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
```

- [ ] **Step 3: Create `mcp.js`**

```js
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
```

- [ ] **Step 4: Verify imports compile**

```bash
node --check frontend/src/services/llm/backend.js
node --check frontend/src/services/llm/anthropic.js
node --check frontend/src/services/llm/mcp.js
```

Expected: no syntax errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/services/llm/
git commit -m "feat(frontend): LLM backend interface with Anthropic and MCP implementations"
```

---

## Task 13: Frontend — main CSS

**Files:**
- Create: `frontend/src/ui/main.css`

Minimal Anthropic-ish palette: warm neutral background, dark slate text, single rust accent. Mobile-first.

- [ ] **Step 1: Create `main.css`**

```css
:root {
  --bg: #f5f1e8;
  --card: #ffffff;
  --text: #2d2a26;
  --muted: #76706a;
  --accent: #c84e1a;
  --border: #e3ddd0;
  --danger: #b3261e;
  --radius: 12px;
}
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; background: var(--bg); color: var(--text); font: 16px/1.5 -apple-system, system-ui, "Segoe UI", sans-serif; }
.app { max-width: 640px; margin: 24px auto; padding: 0 16px; }
.card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); padding: 20px; }
.segmented { display: flex; gap: 4px; background: var(--border); padding: 4px; border-radius: var(--radius); margin-bottom: 16px; }
.segmented button { flex: 1; background: transparent; border: 0; padding: 8px 12px; border-radius: 8px; cursor: pointer; color: var(--muted); }
.segmented button.active { background: var(--card); color: var(--text); font-weight: 600; }
label { display: block; margin: 12px 0 4px; font-size: 14px; color: var(--muted); }
input, select, textarea { width: 100%; padding: 10px; border: 1px solid var(--border); border-radius: 8px; font: inherit; background: #fff; color: var(--text); }
textarea { min-height: 140px; resize: vertical; }
button.primary { background: var(--accent); color: #fff; border: 0; padding: 10px 16px; border-radius: 8px; font: inherit; cursor: pointer; }
button.primary:disabled { opacity: 0.5; cursor: not-allowed; }
.toast { position: fixed; bottom: 20px; left: 50%; transform: translateX(-50%); background: var(--text); color: #fff; padding: 10px 16px; border-radius: 8px; }
.toast.error { background: var(--danger); }
.result { margin-top: 12px; padding: 12px; background: #faf7f0; border-radius: 8px; border: 1px solid var(--border); }
.muted { color: var(--muted); font-size: 14px; }
a { color: var(--accent); }
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/ui/main.css
git commit -m "feat(frontend): main stylesheet with Anthropic-ish palette"
```

---

## Task 14: Frontend — `setup.js` page

**Files:**
- Create: `frontend/src/pages/setup.js`

Setup page asks for PAT, owner/repo, optional Anthropic key. Validates by calling `/user` and `/repos/:o/:r`. Seeds `INDEX.md`/`GENERAL.md`/`UNSURE.md` if repo is empty.

- [ ] **Step 1: Create `setup.js`**

```js
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
      // ensure index/general/unsure exist
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
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/pages/setup.js
git commit -m "feat(frontend): setup page (PAT, repo, optional Anthropic key) with repo seeding"
```

---

## Task 15: Frontend — `remember.js` page

**Files:**
- Create: `frontend/src/pages/remember.js`

- [ ] **Step 1: Create `remember.js`**

```js
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
    try {
      const text = $("#text").value.trim();
      if (!text) { toast("Type or upload some text first.", true); return; }
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
      $("#preview").innerHTML = `<div class="result"><b>${result.short_title}</b><br>${result.one_sentence_summary}<br>tags: ${result.tags}<br><small>${result.unsure ? "low confidence — saving to UNSURE.md" : ""}</small></div>`;
      $("#save").dataset.preset = JSON.stringify(result);
    } catch (e) {
      toast("Auto-categorize failed: " + e.message, true);
    }
  };

  $("#save").onclick = async () => {
    try {
      const text = $("#text").value.trim();
      if (!text) { toast("Empty memory.", true); return; }
      const type = $("#type").value;
      const scopeSel = $("#scope").value;
      const scope = scopeSel === "Team" ? "Team" : `Personal:${config.username}`;
      const preset = $("#save").dataset.preset ? JSON.parse($("#save").dataset.preset) : null;
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
      // If file is new to INDEX.md, add the line.
      const idxFile = await getIndex(gh, cache, /*forceFresh*/ true);
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
    }
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
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/pages/remember.js
git commit -m "feat(frontend): Remember page with auto-categorize, manual fallback, and INDEX.md update"
```

---

## Task 16: Frontend — `lookup.js` page

**Files:**
- Create: `frontend/src/pages/lookup.js`

- [ ] **Step 1: Create `lookup.js`**

```js
import { GitHubClient } from "../services/github.js";
import { Cache } from "../services/cache.js";
import { parseIndex } from "../services/indexmd.js";

export function renderLookup(root, { config, toast }) {
  const gh = new GitHubClient({ token: config.token, owner: config.owner, repo: config.repo });
  const cache = new Cache("team-memory");

  root.innerHTML = `
    <div class="card">
      <h2>Lookup</h2>
      <label>Search</label>
      <input id="q" type="text" placeholder="keywords across INDEX and entry headers" />
      <div id="results" style="margin-top:12px;"></div>
    </div>
  `;
  const $ = s => root.querySelector(s);
  let timer;
  $("#q").oninput = () => {
    clearTimeout(timer);
    timer = setTimeout(() => doSearch($("#q").value, $("#results"), gh, cache, config, toast), 200);
  };
}

async function doSearch(query, out, gh, cache, config, toast) {
  query = query.trim().toLowerCase();
  if (!query) { out.innerHTML = ""; return; }
  try {
    let idxFile = await cache.get("file:INDEX.md");
    if (!idxFile) {
      idxFile = await gh.getFile("INDEX.md");
      await cache.set("file:INDEX.md", idxFile);
    }
    const idx = parseIndex(idxFile.content);
    const fileHits = idx.entries.filter(e =>
      e.path.toLowerCase().includes(query) ||
      e.topics.toLowerCase().includes(query)
    );
    // also fetch up to 5 candidate files and grep entry headers
    const entryHits = [];
    for (const e of fileHits.slice(0, 5)) {
      const cached = await cache.get("file:" + e.path) ?? await fetchAndCache(gh, cache, e.path);
      const lines = cached.content.split(/\r?\n/);
      lines.forEach((l, i) => {
        if (l.startsWith("### Entry:") && l.toLowerCase().includes(query)) {
          entryHits.push({ path: e.path, header: l, line: i });
        }
      });
    }
    out.innerHTML = renderResults(config, fileHits, entryHits);
  } catch (e) {
    toast("Lookup failed: " + e.message, true);
  }
}

async function fetchAndCache(gh, cache, path) {
  const f = await gh.getFile(path);
  await cache.set("file:" + path, f);
  return f;
}

function renderResults(config, fileHits, entryHits) {
  const url = (p, line) => `https://github.com/${config.owner}/${config.repo}/blob/master/${p}${line ? `#L${line + 1}` : ""}`;
  const fileList = fileHits.map(e => `<li><a href="${url(e.path)}" target="_blank">${e.path}</a> <span class="muted">${e.topics}</span></li>`).join("");
  const entryList = entryHits.map(h => `<li><a href="${url(h.path, h.line)}" target="_blank">${h.header}</a> <span class="muted">${h.path}</span></li>`).join("");
  return `
    <div class="result">
      <b>Files (${fileHits.length})</b>
      <ul>${fileList || "<li class='muted'>none</li>"}</ul>
      <b>Entries (${entryHits.length})</b>
      <ul>${entryList || "<li class='muted'>none</li>"}</ul>
    </div>
  `;
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/pages/lookup.js
git commit -m "feat(frontend): Lookup page with text search over INDEX and entry headers"
```

---

## Task 17: Frontend — app shell (`index.html` + `app.js`)

**Files:**
- Create: `frontend/index.html`
- Create: `frontend/src/app.js`

- [ ] **Step 1: Create `index.html`**

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>team-memory</title>
  <link rel="stylesheet" href="./src/ui/main.css" />
</head>
<body>
  <div class="app">
    <h1>team-memory</h1>
    <div class="segmented" id="nav">
      <button data-page="remember">Remember</button>
      <button data-page="lookup">Lookup</button>
      <button data-page="setup">Setup</button>
    </div>
    <div id="root"></div>
  </div>
  <script type="module" src="./src/app.js"></script>
</body>
</html>
```

- [ ] **Step 2: Create `app.js`**

```js
import { renderSetup } from "./pages/setup.js";
import { renderRemember } from "./pages/remember.js";
import { renderLookup } from "./pages/lookup.js";

const CONFIG_KEY = "team-memory:config";

function loadConfig() {
  try { return JSON.parse(localStorage.getItem(CONFIG_KEY)) || {}; } catch { return {}; }
}
function saveConfig(c) { localStorage.setItem(CONFIG_KEY, JSON.stringify(c)); }

function toast(msg, isError = false) {
  const t = document.createElement("div");
  t.className = "toast" + (isError ? " error" : "");
  t.textContent = msg;
  document.body.appendChild(t);
  setTimeout(() => t.remove(), 3500);
}

const root = document.getElementById("root");
const nav = document.getElementById("nav");

function go(page) {
  const config = loadConfig();
  if (page !== "setup" && (!config.token || !config.owner)) {
    page = "setup";
  }
  for (const b of nav.querySelectorAll("button")) {
    b.classList.toggle("active", b.dataset.page === page);
  }
  if (page === "setup") {
    renderSetup(root, {
      config,
      onDone: c => { saveConfig(c); toast("Setup saved."); go("remember"); },
    });
  } else if (page === "remember") {
    renderRemember(root, { config, toast });
  } else {
    renderLookup(root, { config, toast });
  }
}

nav.addEventListener("click", e => {
  const b = e.target.closest("button[data-page]");
  if (b) go(b.dataset.page);
});

go("remember");
```

- [ ] **Step 3: Manual smoke**

```bash
make prompts
npm run serve
# open http://localhost:8080 in browser
# - Setup page should show
# - Enter PAT + repo + optionally an Anthropic key
# - Save & verify; expect "Authenticated as <login>"
# - Switch to Remember, type "test memory", click Save
# - Open the repo on GitHub; confirm GENERAL.md has a new ### Entry block
# - Switch to Lookup, type "test"; expect a hit
```

- [ ] **Step 4: Commit**

```bash
git add frontend/index.html frontend/src/app.js
git commit -m "feat(frontend): app shell with router, nav, and toast"
```

---

## Task 18: README + smoke checklist

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Replace `README.md`**

````markdown
# team-memory

Local-first memory app. All data lives in your own GitHub repo as Markdown.
Slice 1 ships a static web UI plus an optional local Go MCP (`team-memory-mcp`)
that wraps the `claude` CLI so you can use the app without an Anthropic API key.

See `docs/superpowers/specs/2026-05-02-team-memory-slice-1-design.md` for the
full design and `ROADMAP.md` for what's coming next.

## Prereqs

- Node 20+ (only for `npm test` and `python -m http.server`; no build step)
- Go 1.22+ (only if you want the local MCP)
- `claude` CLI on PATH (only if MCP needs to call the LLM with no API key)
- A GitHub repo and a fine-grained PAT with `contents:write` on that repo

## Quickstart (browser-only, with Anthropic key)

```bash
make prompts
npm run serve
# open http://localhost:8080
# Setup page: paste PAT, owner/repo, Anthropic API key
```

## Quickstart (browser + local MCP, no Anthropic key)

```bash
make prompts
make mcp
./mcp/team-memory-mcp &
npm run serve
# open http://localhost:8080
# Setup page: paste PAT, owner/repo; leave Anthropic key blank
```

## Tests

```bash
make test           # runs frontend + MCP tests
```

## Smoke checklist

1. Setup page accepts PAT + repo, shows "Authenticated as <login>".
2. If repo was empty, INDEX.md / GENERAL.md / UNSURE.md appear in GitHub.
3. Remember page → type "test memory" → Save → entry appears in GENERAL.md
   on GitHub with the right `### Entry:` block.
4. Auto-categorize button (with Anthropic key OR MCP running) returns a
   target file and summary; saving commits to that file and updates INDEX.md.
5. Lookup page finds the new entry by keyword.
6. Pulling the repo from GitHub directly while save is in flight should
   produce a "file changed in GitHub" error after 3 retries (manual test).

## Privacy

- PAT and Anthropic key are stored in your browser's `localStorage`. Treat the
  browser profile as the trust boundary. (Web Crypto encryption is on the
  ROADMAP.)
- The MCP binds `127.0.0.1` only.
- No telemetry, no third-party scripts.
````

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: README with quickstart, smoke checklist, and privacy notes"
```

---

## Self-review notes

**Spec coverage:**
- Setup / PAT / username detection → Task 14
- Remember (manual + auto) → Task 15
- Lookup (text search) → Task 16
- IndexedDB cache → Task 11
- INDEX.md parse/update → Task 8
- Entry template → Task 9
- GitHub SHA-retry commit + conflict modal classification → Task 10 + Task 15 (toast handles modal-equivalent UX for slice 1)
- LLM backend abstraction → Task 12
- MCP `/health` + `/v1/categorize` + `/v1/summarize` + stream-json + `claude` subprocess → Tasks 3–7
- Embedded prompts (single source of truth) → Task 2 + 4
- README + smoke checklist → Task 18

**Type/name consistency check:** verified `commitFile`, `putContent`, `getFile`, `getUser` used consistently in `github.js` and `setup.js`/`remember.js`/`lookup.js`. Entry shape (`shortTitle`, `bullets`, etc.) matches between `entries.js` test and `remember.js` consumer.

**Known limitation accepted by design:** Conflict UX in slice 1 is a toast, not a modal with [Open in GitHub] / [Discard] / [Try again] buttons as described in the spec. This is a deliberate slice 1 simplification — bumping it to a real modal is a small follow-up but doesn't change behavior. Captured here as a one-task follow-up if user wants it before slice 1 is "done".
