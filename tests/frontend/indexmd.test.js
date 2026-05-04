import { test } from "node:test";
import assert from "node:assert/strict";
import { parseIndex, serializeIndex, upsertEntry } from "../../frontend/src/services/indexmd.js";

const SAMPLE = `# INDEX for team-memory
shared/programming-practices.md | shared | programming practices; code review; style
GENERAL.md | shared | general
UNSURE.md | shared | unsure
`;

test("parseIndex extracts entries", () => {
  const idx = parseIndex(SAMPLE);
  assert.equal(idx.entries.length, 3);
  assert.deepEqual(idx.entries[0], {
    path: "shared/programming-practices.md",
    scope: "shared",
    topics: "programming practices; code review; style",
  });
});

test("serializeIndex round-trips", () => {
  const idx = parseIndex(SAMPLE);
  const out = serializeIndex(idx);
  assert.equal(parseIndex(out).entries.length, 3);
});

test("upsertEntry adds a new entry", () => {
  const idx = parseIndex(SAMPLE);
  upsertEntry(idx, { path: "shared/new-topic.md", scope: "shared", topics: "new; stuff" });
  assert.equal(idx.entries.length, 4);
  assert.equal(idx.entries[3].path, "shared/new-topic.md");
});

test("upsertEntry replaces existing", () => {
  const idx = parseIndex(SAMPLE);
  upsertEntry(idx, { path: "GENERAL.md", scope: "shared", topics: "general; misc" });
  assert.equal(idx.entries.length, 3);
  assert.equal(idx.entries.find(e => e.path === "GENERAL.md").topics, "general; misc");
});

test("parseIndex tolerates blank lines and missing header", () => {
  const idx = parseIndex("\nGENERAL.md | shared | general\n\n");
  assert.equal(idx.entries.length, 1);
});
