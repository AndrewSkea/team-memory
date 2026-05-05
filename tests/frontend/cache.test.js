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
