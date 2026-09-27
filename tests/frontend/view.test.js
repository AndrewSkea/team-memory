import { test } from "node:test";
import assert from "node:assert/strict";
import { ago, githubUrl, memoryCard, provenance, splitList, typeLabel } from "../../frontend/src/lib/view.js";

const now = new Date("2026-09-27T12:00:00Z");

test("ago", () => {
  assert.equal(ago("2026-09-27T11:59:30Z", now), "just now");
  assert.equal(ago("2026-09-27T10:00:00Z", now), "2 hours ago");
  assert.equal(ago("2026-09-20T12:00:00Z", now), "7 days ago");
  assert.equal(ago("2026-08-01T12:00:00Z", now), "8 weeks ago");
  assert.equal(ago("0001-01-01T00:00:00Z", now), "");
  assert.equal(ago("garbage", now), "");
});

test("githubUrl encodes segments", () => {
  assert.equal(githubUrl({ owner: "acme", repo: "kb" }, "memories/2026/09/a b.md"),
    "https://github.com/acme/kb/blob/HEAD/memories/2026/09/a%20b.md");
  assert.equal(githubUrl({}, "x.md"), "");
});

test("memoryCard escapes everything", () => {
  const evil = '<img src=x onerror=alert(1)>';
  const html = memoryCard({ id: '"><script>', title: evil, summary: evil, body: evil, about: [evil], type: evil,
    learned_in: evil, author: evil, created: "2026-09-01T00:00:00Z", path: "memories/x.md" }, { settings: { owner: "o", repo: "r" }, now });
  assert.ok(!html.includes("<img"), html);
  assert.ok(!html.includes("<script>"), html);
  assert.ok(html.includes("&lt;img"));
});

test("memoryCard shows provenance, status and actions", () => {
  const m = { id: "1", title: "T", about: ["payments-api"], learned_in: "acme/billing", author: "alice", created: "2026-09-26T12:00:00Z", path: "memories/1.md" };
  const html = memoryCard(m, { settings: { owner: "o", repo: "r" }, now });
  assert.match(html, /learned in acme\/billing · by alice · 24 hours ago/);
  assert.match(html, /data-about="payments-api"/);
  assert.match(html, /data-forget="1"/);
  const retired = memoryCard({ ...m, status: "superseded" }, { now });
  assert.match(retired, /inactive/);
  assert.ok(!retired.includes("data-forget"));
  assert.ok(!memoryCard({ ...m, legacy: true }, { now }).includes("data-forget"));
});

test("helpers", () => {
  assert.deepEqual(splitList(" a, ,b ,"), ["a", "b"]);
  assert.equal(typeLabel("howto"), "How-to");
  assert.equal(typeLabel("custom"), "custom");
  assert.equal(provenance({}, now), "");
});
