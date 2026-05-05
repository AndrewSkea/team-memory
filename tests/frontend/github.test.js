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
