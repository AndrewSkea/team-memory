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
