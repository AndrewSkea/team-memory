import { test } from "node:test";
import assert from "node:assert/strict";
import { createApi, ApiError } from "../../frontend/src/services/api.js";

function fakeFetch(responses) {
  const calls = [];
  const f = async (path, opts) => {
    calls.push({ path, opts });
    const r = responses.shift() ?? { status: 200, body: {} };
    return { ok: r.status < 400, status: r.status, text: async () => typeof r.body === "string" ? r.body : JSON.stringify(r.body) };
  };
  return { f, calls };
}

test("search builds query string", async () => {
  const { f, calls } = fakeFetch([{ status: 200, body: { results: [] } }]);
  const api = createApi(f);
  await api.search({ q: "payments api", about: ["a", "b"], type: "gotcha", all: true, limit: 5 });
  assert.equal(calls[0].path, "/v1/memories?q=payments+api&about=a%2Cb&type=gotcha&all=1&limit=5");
  assert.equal(calls[0].opts.method, "GET");
  assert.equal(calls[0].opts.body, undefined);
});

test("writes send JSON with content type", async () => {
  const { f, calls } = fakeFetch([{ status: 200, body: { memory: {} } }]);
  await createApi(f).remember({ text: "x" });
  assert.equal(calls[0].opts.method, "POST");
  assert.equal(calls[0].opts.headers["Content-Type"], "application/json");
  assert.deepEqual(JSON.parse(calls[0].opts.body), { text: "x" });
});

test("ids are URL-encoded", async () => {
  const { f, calls } = fakeFetch([{ status: 200, body: {} }]);
  await createApi(f).forget("a/../b", "r");
  assert.equal(calls[0].path, "/v1/memories/a%2F..%2Fb/forget");
});

test("errors carry server message and status", async () => {
  const { f } = fakeFetch([{ status: 503, body: { error: "team-memory is not set up yet" } }]);
  await assert.rejects(createApi(f).settings(), e => e instanceof ApiError && e.status === 503 && /not set up/.test(e.message));
  const bad = createApi(async () => { throw new TypeError("network"); });
  await assert.rejects(bad.health(), /Can't reach team-memory/);
  const { f: f2 } = fakeFetch([{ status: 500, body: "plain text" }]);
  await assert.rejects(createApi(f2).stats(), /plain text/);
});
