// Thin client for the local team-memory API. The browser never talks to
// GitHub or holds the token: the local server does both.

export class ApiError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

export function createApi(fetchImpl = (...a) => globalThis.fetch(...a)) {
  async function request(method, path, body) {
    const opts = { method, headers: {} };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    let res;
    try {
      res = await fetchImpl(path, opts);
    } catch {
      throw new ApiError(0, "Can't reach team-memory — is it still running?");
    }
    const text = await res.text();
    let data = {};
    try { data = text ? JSON.parse(text) : {}; } catch { data = { error: text }; }
    if (!res.ok) throw new ApiError(res.status, data.error || `HTTP ${res.status}`);
    return data;
  }
  const enc = encodeURIComponent;
  return {
    health: () => request("GET", "/health"),
    settings: () => request("GET", "/v1/settings"),
    setMode: mode => request("PUT", "/v1/settings", { mode }),
    setup: input => request("POST", "/v1/setup", input),
    sync: () => request("POST", "/v1/sync", {}),
    search: ({ q = "", about = [], type = "", limit = 50, all = false } = {}) => {
      const p = new URLSearchParams();
      if (q) p.set("q", q);
      if (about.length) p.set("about", about.join(","));
      if (type) p.set("type", type);
      if (all) p.set("all", "1");
      p.set("limit", String(limit));
      return request("GET", `/v1/memories?${p}`);
    },
    get: id => request("GET", `/v1/memories/${enc(id)}`),
    remember: input => request("POST", "/v1/memories", input),
    forget: (id, reason) => request("POST", `/v1/memories/${enc(id)}/forget`, { reason }),
    supersede: (id, input) => request("POST", `/v1/memories/${enc(id)}/supersede`, input),
    pending: () => request("GET", "/v1/pending"),
    approve: (id, edits = {}) => request("POST", `/v1/pending/${enc(id)}/approve`, edits),
    reject: id => request("POST", `/v1/pending/${enc(id)}/reject`, {}),
    stats: () => request("GET", "/v1/stats"),
    entities: () => request("GET", "/v1/entities"),
  };
}

export const api = createApi();
