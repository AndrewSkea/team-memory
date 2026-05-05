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
