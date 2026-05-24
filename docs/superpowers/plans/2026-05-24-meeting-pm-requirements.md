# Meeting PM & Requirements Generation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a floating meeting drawer with per-series history, AI note enrichment, and JIRA-style requirements generation backed by GitHub-stored markdown.

**Architecture:** All changes in `frontend/src/` (pure JS ES modules). Two new services (`meetings-series.js`, `requirements-gen.js` via extending anthropic.js). One new component (`meeting-drawer.js`). Meetings page becomes a series browser. Both `frontend/` and `mcp/frontend/` updated — they are independent trees; `copy-prompts.sh` copies JS but would destroy mcp/frontend extras, so mirror changes manually. No Go changes.

**Tech Stack:** Vanilla JS ES modules, Node.js built-in test runner (`node:test`), existing `GitHubClient` and `AnthropicDirect`, `parseMsgFile` (already exists for .msg parsing).

---

## File Map

| Action | Path | Purpose |
|--------|------|---------|
| Create | `frontend/src/services/meetings-series.js` | Slug derivation, series file read/write, occurrence formatting |
| Create | `frontend/src/components/meeting-drawer.js` | Floating FAB + drawer, all drawer state |
| Create | `prompts/enrich-meeting.txt` | AI enrichment system prompt (source of truth) |
| Create | `prompts/generate-requirements.txt` | Requirements generation system prompt (source of truth) |
| Create | `tests/frontend/meetings-series.test.js` | Unit tests for pure functions |
| Create | `tests/frontend/llm-enrich.test.js` | Unit tests for new AnthropicDirect methods |
| Modify | `frontend/src/services/llm/anthropic.js` | Add `_callText`, `enrich`, `generateRequirements` |
| Modify | `frontend/src/services/llm/mcp.js` | Add `enrich`/`generateRequirements` stubs (not supported) |
| Modify | `frontend/src/services/github.js` | Add `getDir` method |
| Modify | `frontend/src/pages/meetings.js` | Replace form with series browser |
| Modify | `frontend/src/pages/setup.js` | Add codebase repo config fields |
| Modify | `frontend/src/app.js` | Init drawer on bootstrap, pass `llm` to drawer |
| Modify | `frontend/index.html` | Add `<div id="meeting-drawer-root">` |
| Modify | `frontend/src/ui/main.css` | Drawer + FAB styles |
| Mirror | `mcp/frontend/src/` | Apply identical changes to mcp frontend tree |

---

## Task 1: `meetings-series.js` — slug + series file format (pure functions)

**Files:**
- Create: `frontend/src/services/meetings-series.js`
- Create: `tests/frontend/meetings-series.test.js`

- [ ] **Step 1: Write the failing tests**

```js
// tests/frontend/meetings-series.test.js
import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import {
  deriveSlug,
  parseSeries,
  buildSeriesFile,
  formatOccurrence,
  appendOccurrence,
} from '../../frontend/src/services/meetings-series.js';

describe('deriveSlug', () => {
  it('strips RE: prefix', () => {
    assert.equal(deriveSlug('RE: Sprint Planning'), 'sprint-planning');
  });
  it('strips FW: prefix', () => {
    assert.equal(deriveSlug('FW: Sprint Planning'), 'sprint-planning');
  });
  it('strips FWD: prefix', () => {
    assert.equal(deriveSlug('FWD: Sprint Planning'), 'sprint-planning');
  });
  it('strips trailing day-date suffix', () => {
    assert.equal(deriveSlug('Sprint Planning - Mon 24 May 2026'), 'sprint-planning');
  });
  it('strips trailing (2) occurrence suffix', () => {
    assert.equal(deriveSlug('Sprint Planning (2)'), 'sprint-planning');
  });
  it('lowercases and hyphenates', () => {
    assert.equal(deriveSlug('Incident Review'), 'incident-review');
  });
  it('handles combined prefix + suffix', () => {
    assert.equal(deriveSlug('RE: Sprint Planning - Mon 24 May 2026'), 'sprint-planning');
  });
});

describe('parseSeries', () => {
  const content = `---
title: Sprint Planning
slug: sprint-planning
attendees: Alice, Bob
template: Sprint Planning
created: 2026-01-10
---

## 2026-05-10
**Notes:** Old notes
**Decisions:** Old decision
**Action Items:** Alice: old task
**AI Enrichment:** 

## 2026-05-24
**Notes:** New notes
**Decisions:** New decision
**Action Items:** Bob: new task
**AI Enrichment:** 
`;

  it('parses frontmatter', () => {
    const r = parseSeries(content);
    assert.equal(r.frontmatter.title, 'Sprint Planning');
    assert.equal(r.frontmatter.slug, 'sprint-planning');
    assert.equal(r.frontmatter.attendees, 'Alice, Bob');
  });

  it('parses occurrences in order (newest first)', () => {
    const r = parseSeries(content);
    assert.equal(r.occurrences.length, 2);
    assert.equal(r.occurrences[0].date, '2026-05-24');
    assert.equal(r.occurrences[1].date, '2026-05-10');
  });

  it('parses occurrence fields', () => {
    const r = parseSeries(content);
    assert.equal(r.occurrences[0].notes.trim(), 'New notes');
    assert.equal(r.occurrences[0].decisions.trim(), 'New decision');
    assert.equal(r.occurrences[0].actions.trim(), 'Bob: new task');
  });

  it('returns empty occurrences for frontmatter-only content', () => {
    const r = parseSeries(`---\ntitle: X\nslug: x\nattendees: \ntemplate: Blank\ncreated: 2026-01-01\n---\n`);
    assert.equal(r.occurrences.length, 0);
  });
});

describe('buildSeriesFile', () => {
  it('produces valid frontmatter', () => {
    const result = buildSeriesFile({ title: 'Sprint Planning', slug: 'sprint-planning', attendees: 'Alice', template: 'Sprint Planning' });
    assert.ok(result.startsWith('---\n'));
    assert.ok(result.includes('title: Sprint Planning'));
    assert.ok(result.includes('slug: sprint-planning'));
    assert.ok(result.endsWith('---\n'));
  });
});

describe('formatOccurrence', () => {
  it('produces a dated section with all fields', () => {
    const result = formatOccurrence({ date: '2026-05-24', notes: 'note', decisions: 'dec', actions: 'Alice: task', enrichment: '' });
    assert.ok(result.includes('## 2026-05-24'));
    assert.ok(result.includes('**Notes:** note'));
    assert.ok(result.includes('**Decisions:** dec'));
    assert.ok(result.includes('**Action Items:** Alice: task'));
  });

  it('includes enrichment section when provided', () => {
    const result = formatOccurrence({ date: '2026-05-24', notes: '', decisions: '', actions: '', enrichment: 'AI says hi' });
    assert.ok(result.includes('**AI Enrichment:** AI says hi'));
  });
});

describe('appendOccurrence', () => {
  it('appends a new occurrence to existing content', () => {
    const base = '---\ntitle: T\nslug: t\nattendees: \ntemplate: Blank\ncreated: 2026-01-01\n---\n';
    const occ = { date: '2026-05-24', notes: 'n', decisions: 'd', actions: 'a', enrichment: '' };
    const result = appendOccurrence(base, occ);
    assert.ok(result.includes('## 2026-05-24'));
    assert.ok(result.startsWith('---\n'));
  });
});
```

- [ ] **Step 2: Run tests — verify they fail**

```
node --test tests/frontend/meetings-series.test.js
```
Expected: `ReferenceError` or `Cannot find module`

- [ ] **Step 3: Implement `meetings-series.js`**

```js
// frontend/src/services/meetings-series.js

export function deriveSlug(subject) {
  return subject
    .replace(/^(re|fw|fwd)\s*:\s*/i, '')
    .replace(/\s*[-–—]\s*(mon|tue|wed|thu|fri|sat|sun)\s+\d+.*/i, '')
    .replace(/\s*\(\d+\)$/, '')
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

export function parseSeries(content) {
  const fmMatch = content.match(/^---\n([\s\S]*?)\n---\n/);
  const frontmatter = {};
  if (fmMatch) {
    for (const line of fmMatch[1].split('\n')) {
      const idx = line.indexOf(':');
      if (idx === -1) continue;
      frontmatter[line.slice(0, idx).trim()] = line.slice(idx + 1).trim();
    }
  }

  const body = fmMatch ? content.slice(fmMatch[0].length) : content;
  const sections = body.split(/\n(?=## \d{4}-\d{2}-\d{2})/).filter(s => s.trim());
  const occurrences = sections.map(s => {
    const dateMatch = s.match(/^## (\d{4}-\d{2}-\d{2})/);
    const date = dateMatch ? dateMatch[1] : '';
    return {
      date,
      notes:      extractField(s, 'Notes'),
      decisions:  extractField(s, 'Decisions'),
      actions:    extractField(s, 'Action Items'),
      enrichment: extractField(s, 'AI Enrichment'),
    };
  }).sort((a, b) => b.date.localeCompare(a.date));

  return { frontmatter, occurrences };
}

function extractField(section, label) {
  const re = new RegExp(`\\*\\*${label}:\\*\\*\\s*([\\s\\S]*?)(?=\\n\\*\\*|$)`);
  const m = section.match(re);
  return m ? m[1].trim() : '';
}

export function buildSeriesFile({ title, slug, attendees, template }) {
  const created = new Date().toISOString().slice(0, 10);
  return `---\ntitle: ${title}\nslug: ${slug}\nattendees: ${attendees}\ntemplate: ${template}\ncreated: ${created}\n---\n`;
}

export function formatOccurrence({ date, notes, decisions, actions, enrichment }) {
  return [
    `\n## ${date}`,
    `**Notes:** ${notes}`,
    `**Decisions:** ${decisions}`,
    `**Action Items:** ${actions}`,
    `**AI Enrichment:** ${enrichment ?? ''}`,
    '',
  ].join('\n');
}

export function appendOccurrence(content, occurrence) {
  return content + formatOccurrence(occurrence);
}
```

- [ ] **Step 4: Run tests — verify they pass**

```
node --test tests/frontend/meetings-series.test.js
```
Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add frontend/src/services/meetings-series.js tests/frontend/meetings-series.test.js
git commit -m "feat: add meetings-series service with slug derivation and series file format"
```

---

## Task 2: Extend `AnthropicDirect` with text-mode AI methods

**Files:**
- Modify: `frontend/src/services/llm/anthropic.js`
- Modify: `frontend/src/services/llm/mcp.js`
- Create: `tests/frontend/llm-enrich.test.js`

- [ ] **Step 1: Write failing tests**

```js
// tests/frontend/llm-enrich.test.js
import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { AnthropicDirect } from '../../frontend/src/services/llm/anthropic.js';

function makeFetch(text) {
  return async () => ({
    ok: true,
    json: async () => ({ content: [{ type: 'text', text }] }),
    text: async () => text,
  });
}

function makeFailFetch(status) {
  return async () => ({ ok: false, status, text: async () => 'error' });
}

describe('AnthropicDirect.enrich', () => {
  it('returns enriched text string', async () => {
    const llm = new AnthropicDirect({
      apiKey: 'key',
      loadPrompt: async () => 'sys',
      fetch: makeFetch('enriched notes here'),
    });
    const result = await llm.enrich({ seriesContent: '', currentNotes: 'raw', indexContent: '', codebaseContent: '' });
    assert.equal(typeof result, 'string');
    assert.equal(result, 'enriched notes here');
  });

  it('throws on non-ok response', async () => {
    const llm = new AnthropicDirect({
      apiKey: 'key',
      loadPrompt: async () => 'sys',
      fetch: makeFailFetch(429),
    });
    await assert.rejects(() => llm.enrich({ seriesContent: '', currentNotes: '', indexContent: '', codebaseContent: '' }), /anthropic 429/);
  });
});

describe('AnthropicDirect.generateRequirements', () => {
  it('returns requirements markdown string', async () => {
    const llm = new AnthropicDirect({
      apiKey: 'key',
      loadPrompt: async () => 'sys',
      fetch: makeFetch('## TICKET-001: Do thing\n**Priority:** P1'),
    });
    const result = await llm.generateRequirements({ occurrencesContent: '', indexContent: '', codebaseContent: '' });
    assert.ok(result.includes('TICKET-001'));
  });
});
```

- [ ] **Step 2: Run tests — verify they fail**

```
node --test tests/frontend/llm-enrich.test.js
```
Expected: `TypeError: llm.enrich is not a function`

- [ ] **Step 3: Add `_callText`, `enrich`, `generateRequirements` to `AnthropicDirect`**

In `frontend/src/services/llm/anthropic.js`, add after the existing `_call` method:

```js
  async _callText(systemPrompt, userContent) {
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
        max_tokens: 4096,
        system: systemPrompt,
        messages: [{ role: "user", content: userContent }],
      }),
    });
    if (!res.ok) {
      const t = await res.text().catch(() => "");
      throw new Error(`anthropic ${res.status}: ${t.slice(0, 200)}`);
    }
    const j = await res.json();
    return (j.content ?? []).filter(c => c.type === "text").map(c => c.text).join("");
  }

  async enrich({ seriesContent, currentNotes, indexContent, codebaseContent }) {
    const sys = await this.loadPrompt("enrich-meeting");
    return this._callText(sys, JSON.stringify({ seriesContent, currentNotes, indexContent, codebaseContent }));
  }

  async generateRequirements({ occurrencesContent, indexContent, codebaseContent }) {
    const sys = await this.loadPrompt("generate-requirements");
    return this._callText(sys, JSON.stringify({ occurrencesContent, indexContent, codebaseContent }));
  }
```

- [ ] **Step 4: Add stubs to `LocalMCP`**

In `frontend/src/services/llm/mcp.js`, add inside the `LocalMCP` class:

```js
  async enrich() {
    throw new Error("Note enrichment requires an Anthropic API key — add one in Setup.");
  }

  async generateRequirements() {
    throw new Error("Requirements generation requires an Anthropic API key — add one in Setup.");
  }
```

- [ ] **Step 5: Run tests — verify they pass**

```
node --test tests/frontend/llm-enrich.test.js
```
Expected: all pass

- [ ] **Step 6: Run all frontend tests to check no regressions**

```
npm test
```
Expected: all pass

- [ ] **Step 7: Commit**

```bash
git add frontend/src/services/llm/anthropic.js frontend/src/services/llm/mcp.js tests/frontend/llm-enrich.test.js
git commit -m "feat: add enrich and generateRequirements methods to AnthropicDirect"
```

---

## Task 3: Add `getDir` to `GitHubClient`

**Files:**
- Modify: `frontend/src/services/github.js`

- [ ] **Step 1: Add `getDir` method**

In `frontend/src/services/github.js`, add inside the `GitHubClient` class after `putContent`:

```js
  async getDir(path) {
    const r = await this._req(`/repos/${this.owner}/${this.repo}/contents/${encodeURIComponent(path)}`);
    if (r.status === 404) return [];
    if (!r.ok) throw new Error(`getDir ${path}: ${r.status}`);
    return r.json();
  }
```

- [ ] **Step 2: Run all frontend tests**

```
npm test
```
Expected: all pass

- [ ] **Step 3: Commit**

```bash
git add frontend/src/services/github.js
git commit -m "feat: add getDir to GitHubClient for listing repo directories"
```

---

## Task 4: Write AI prompts

**Files:**
- Create: `prompts/enrich-meeting.txt`
- Create: `prompts/generate-requirements.txt`

- [ ] **Step 1: Create `prompts/enrich-meeting.txt`**

```
You are a technical meeting notes assistant. You receive meeting notes and context about the team's codebase and past decisions.

Enrich the provided meeting notes by:
1. Adding context from the series history and codebase knowledge where gaps exist
2. Clarifying vague decisions with reasoning based on past patterns
3. Flagging action items missing owners with [OWNER NEEDED]
4. Adding relevant technical context from the codebase when technology is referenced

You receive a JSON object:
- currentNotes: what the user typed
- seriesContent: full history of this meeting series (past occurrences)
- indexContent: team-memory INDEX.md and key decision files
- codebaseContent: README and relevant files from the codebase repo

Return ONLY the enriched meeting notes as markdown. Preserve the original structure. Do not add preamble or commentary. Expand and improve — do not truncate.
```

- [ ] **Step 2: Create `prompts/generate-requirements.txt`**

```
You are a technical product manager. You receive meeting notes from one or more recurring meeting occurrences, plus codebase and team-decision context.

Generate JIRA-style requirements tickets from the meeting content. Each distinct piece of work becomes one ticket.

Use this exact format for each ticket:

## TICKET-NNN: [Title]
**Priority:** P0|P1|P2|P3
**Story Points:** 1|2|3|5|8
**Labels:** comma, separated, labels
**Description:**
[2-3 sentences describing the work and why it matters]

**Acceptance Criteria:**
- [ ] [Specific, testable criterion]
- [ ] [Specific, testable criterion]

---

Priority: P0=critical/blocking, P1=this sprint, P2=next sprint, P3=backlog
Story points: 1=trivial, 2=small, 3=medium, 5=large, 8=very large (split anything larger)

You receive a JSON object:
- occurrencesContent: the meeting notes (one or more dated occurrences)
- indexContent: team-memory index and decisions
- codebaseContent: codebase README and relevant files

Output ONLY the tickets in the exact format above. No preamble, no summary, no commentary. Number tickets sequentially starting at TICKET-001.
```

- [ ] **Step 3: Copy prompts to frontend and mcp prompt directories**

The `scripts/copy-prompts.sh` script also wipes and rebuilds `mcp/frontend/` which would destroy independent changes. Run only the prompt copy portion:

```bash
cp prompts/enrich-meeting.txt frontend/prompts/enrich-meeting.txt
cp prompts/generate-requirements.txt frontend/prompts/generate-requirements.txt
cp prompts/enrich-meeting.txt mcp/prompts/enrich-meeting.txt
cp prompts/generate-requirements.txt mcp/prompts/generate-requirements.txt
```

- [ ] **Step 4: Add embed directives for new prompts in Go embed file**

In `mcp/prompts/embed.go`, add:

```go
//go:embed enrich-meeting.txt
var EnrichMeeting string

//go:embed generate-requirements.txt
var GenerateRequirements string
```

- [ ] **Step 5: Commit**

```bash
git add prompts/enrich-meeting.txt prompts/generate-requirements.txt
git add -f frontend/prompts/enrich-meeting.txt frontend/prompts/generate-requirements.txt
git add -f mcp/prompts/enrich-meeting.txt mcp/prompts/generate-requirements.txt
git add mcp/prompts/embed.go
git commit -m "feat: add AI prompts for meeting enrichment and requirements generation"
```

---

## Task 5: Drawer CSS

**Files:**
- Modify: `frontend/src/ui/main.css`

- [ ] **Step 1: Append drawer + FAB styles to `frontend/src/ui/main.css`**

```css
/* ── Meeting drawer FAB ── */
.meeting-fab {
  position: fixed;
  bottom: 24px;
  right: 24px;
  width: 52px;
  height: 52px;
  border-radius: 50%;
  background: var(--accent);
  color: #fff;
  border: none;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 4px 16px rgba(200,78,26,0.35), 0 2px 6px rgba(200,78,26,0.2);
  transition: background 0.15s, transform 0.15s;
  z-index: 1000;
}
.meeting-fab:hover { background: var(--accent-hover); transform: scale(1.05); }
.meeting-fab svg { width: 22px; height: 22px; fill: none; stroke: #fff; stroke-width: 2; }

/* ── Drawer overlay ── */
.meeting-overlay {
  position: fixed;
  inset: 0;
  background: rgba(45,42,38,0.3);
  z-index: 1001;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.2s;
}
.meeting-overlay--visible {
  opacity: 1;
  pointer-events: auto;
}

/* ── Drawer panel ── */
.meeting-drawer {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  width: 420px;
  max-width: 100vw;
  background: var(--card);
  box-shadow: -4px 0 32px rgba(45,42,38,0.15);
  z-index: 1002;
  display: flex;
  flex-direction: column;
  transform: translateX(100%);
  transition: transform 0.22s cubic-bezier(0.4,0,0.2,1);
  overflow: hidden;
}
.meeting-drawer--open { transform: translateX(0); }

.meeting-drawer__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 18px 12px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.meeting-drawer__title {
  font-size: 14px;
  font-weight: 700;
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 300px;
}
.meeting-drawer__badge {
  font-size: 11px;
  font-weight: 500;
  color: var(--muted);
  margin-left: 8px;
}
.meeting-drawer__close {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--muted);
  font-size: 20px;
  line-height: 1;
  padding: 2px 6px;
  border-radius: 4px;
  flex-shrink: 0;
}
.meeting-drawer__close:hover { background: var(--inner); color: var(--text); }

/* ── Drawer tabs ── */
.meeting-drawer__tabs {
  display: flex;
  border-bottom: 1px solid var(--border);
  padding: 0 18px;
  flex-shrink: 0;
}
.meeting-drawer__tab {
  flex: 1;
  padding: 10px 0 9px;
  border: none;
  border-bottom: 2.5px solid transparent;
  background: transparent;
  cursor: pointer;
  font: inherit;
  font-size: 13px;
  font-weight: 500;
  color: var(--muted);
}
.meeting-drawer__tab--active {
  color: var(--accent);
  border-bottom-color: var(--accent);
}

/* ── Drawer body ── */
.meeting-drawer__body {
  flex: 1;
  overflow-y: auto;
  padding: 16px 18px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

/* ── History accordion ── */
.meeting-history { margin-bottom: 6px; }
.meeting-history__toggle {
  width: 100%;
  background: var(--inner);
  border: none;
  border-radius: var(--radius-sm);
  padding: 8px 12px;
  font: inherit;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-2);
  cursor: pointer;
  text-align: left;
  display: flex;
  align-items: center;
  gap: 6px;
}
.meeting-history__toggle:hover { background: var(--border); }
.meeting-history__chevron { margin-left: auto; transition: transform 0.15s; }
.meeting-history__item { background: var(--inner); border-radius: var(--radius-sm); padding: 10px 12px; font-size: 12px; margin-top: 4px; display: none; }
.meeting-history__item--open { display: block; }
.meeting-history__item p { color: var(--text-2); line-height: 1.5; }
.meeting-history__item strong { color: var(--text); font-size: 11px; text-transform: uppercase; letter-spacing: 0.06em; }

/* ── Requirements output ── */
.requirements-output {
  background: var(--inner);
  border-radius: var(--radius-sm);
  padding: 12px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--text);
  white-space: pre-wrap;
  min-height: 120px;
  font-family: inherit;
}

/* ── Drawer section label ── */
.drawer-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--muted);
  margin-bottom: 4px;
}

/* ── Scope checkboxes ── */
.scope-row { display: flex; gap: 8px; flex-wrap: wrap; }
.scope-check { display: flex; align-items: center; gap: 4px; font-size: 12px; color: var(--text-2); cursor: pointer; }
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/ui/main.css
git commit -m "feat: add meeting drawer and FAB CSS styles"
```

---

## Task 6: `meeting-drawer.js` component

**Files:**
- Create: `frontend/src/components/meeting-drawer.js`

- [ ] **Step 1: Create directory and component file**

```bash
mkdir -p frontend/src/components
```

```js
// frontend/src/components/meeting-drawer.js
import { parseMsgFile } from '../services/msgparser.js';
import { deriveSlug, parseSeries, buildSeriesFile, formatOccurrence, appendOccurrence } from '../services/meetings-series.js';
import { MEETING_TEMPLATES, applyMeetingTemplate } from '../pages/meetings.js';
import { pickBackend } from '../services/llm/backend.js';

const TODAY = () => new Date().toISOString().slice(0, 10);

export function initMeetingDrawer(config, gh) {
  const root = document.getElementById('meeting-drawer-root');
  if (!root) return;

  root.innerHTML = `
    <button class="meeting-fab" id="mfab" title="Meeting notes">
      <svg viewBox="0 0 24 24"><rect x="3" y="4" width="18" height="18" rx="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/></svg>
    </button>
    <div class="meeting-overlay" id="moverlay"></div>
    <div class="meeting-drawer" id="mdrawer">
      <div class="meeting-drawer__header">
        <span>
          <span class="meeting-drawer__title" id="mdrawer-title">New Meeting</span>
          <span class="meeting-drawer__badge" id="mdrawer-badge"></span>
        </span>
        <button class="meeting-drawer__close" id="mdrawer-close">×</button>
      </div>
      <div class="meeting-drawer__tabs">
        <button class="meeting-drawer__tab meeting-drawer__tab--active" data-drawtab="notes">Notes</button>
        <button class="meeting-drawer__tab" data-drawtab="requirements">Requirements</button>
      </div>
      <div class="meeting-drawer__body" id="mdrawer-body"></div>
    </div>
  `;

  const state = { open: false, tab: 'notes', series: null, slug: null };

  const fab      = document.getElementById('mfab');
  const overlay  = document.getElementById('moverlay');
  const drawer   = document.getElementById('mdrawer');
  const titleEl  = document.getElementById('mdrawer-title');
  const badgeEl  = document.getElementById('mdrawer-badge');
  const body     = document.getElementById('mdrawer-body');

  function openDrawer() {
    state.open = true;
    drawer.classList.add('meeting-drawer--open');
    overlay.classList.add('meeting-overlay--visible');
    renderBody();
  }

  function closeDrawer() {
    state.open = false;
    drawer.classList.remove('meeting-drawer--open');
    overlay.classList.remove('meeting-overlay--visible');
  }

  fab.addEventListener('click', openDrawer);
  overlay.addEventListener('click', closeDrawer);
  document.getElementById('mdrawer-close').addEventListener('click', closeDrawer);

  drawer.addEventListener('click', e => {
    const tab = e.target.closest('[data-drawtab]');
    if (!tab) return;
    state.tab = tab.dataset.drawtab;
    drawer.querySelectorAll('.meeting-drawer__tab').forEach(t =>
      t.classList.toggle('meeting-drawer__tab--active', t.dataset.drawtab === state.tab)
    );
    renderBody();
  });

  async function loadSeries(slug) {
    const file = await gh.getFile(`meetings/${slug}.md`);
    if (!file.exists) { state.series = null; state.slug = slug; return; }
    state.series = parseSeries(file.content);
    state.series._rawContent = file.content;
    state.series._sha = file.sha;
    state.slug = slug;
    titleEl.textContent = state.series.frontmatter.title ?? slug;
    badgeEl.textContent = `· ${state.series.occurrences.length} session${state.series.occurrences.length !== 1 ? 's' : ''}`;
  }

  function renderBody() {
    if (state.tab === 'notes') renderNotesTab();
    else renderRequirementsTab();
  }

  function renderNotesTab() {
    const attendees = state.series?.frontmatter.attendees ?? '';
    const historyHtml = state.series?.occurrences.length
      ? `<div class="meeting-history">
           <p class="drawer-label">Past sessions</p>
           ${state.series.occurrences.map((occ, i) => `
             <button class="meeting-history__toggle" data-histidx="${i}">
               <svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2"><polyline points="4 6 8 10 12 6"/></svg>
               ${occ.date}
               <span class="meeting-history__chevron" id="hist-chev-${i}">›</span>
             </button>
             <div class="meeting-history__item" id="hist-item-${i}">
               ${occ.notes ? `<p><strong>Notes</strong><br>${escHtml(occ.notes)}</p>` : ''}
               ${occ.decisions ? `<p><strong>Decisions</strong><br>${escHtml(occ.decisions)}</p>` : ''}
               ${occ.actions ? `<p><strong>Actions</strong><br>${escHtml(occ.actions)}</p>` : ''}
             </div>
           `).join('')}
         </div>`
      : '';

    body.innerHTML = `
      <div class="drop-zone" id="mdrop" style="margin-bottom:0">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18" style="display:block;margin:0 auto 4px"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
        Drop .msg invite or click
        <input type="file" accept=".msg" id="mdfile" style="display:none">
      </div>
      ${historyHtml}
      <div>
        <p class="drawer-label">Template</p>
        <select id="mdtmpl" class="search-input" style="width:100%;padding-left:12px">
          ${MEETING_TEMPLATES.map(t => `<option value="${t.name}">${t.name}</option>`).join('')}
        </select>
      </div>
      <div>
        <p class="drawer-label">Title</p>
        <input id="mdtitle" class="search-input" type="text" placeholder="Sprint Planning" style="width:100%;padding-left:12px" value="${escHtml(state.series?.frontmatter.title ?? '')}">
      </div>
      <div style="display:flex;gap:8px">
        <div style="flex:1">
          <p class="drawer-label">Date</p>
          <input id="mddate" class="search-input" type="date" value="${TODAY()}" style="width:100%;padding-left:12px">
        </div>
        <div style="flex:2">
          <p class="drawer-label">Attendees</p>
          <input id="mdattendees" class="search-input" type="text" placeholder="Alice, Bob" value="${escHtml(attendees)}" style="width:100%;padding-left:12px">
        </div>
      </div>
      <div>
        <p class="drawer-label">Notes</p>
        <textarea id="mdnotes" class="textarea" style="min-height:80px" placeholder="Key discussion points..."></textarea>
      </div>
      <div>
        <p class="drawer-label">Decisions</p>
        <textarea id="mddecisions" class="textarea" style="min-height:60px" placeholder="We decided to..."></textarea>
      </div>
      <div>
        <p class="drawer-label">Action Items (Owner: task)</p>
        <textarea id="mdactions" class="textarea" style="min-height:60px" placeholder="Alice: set up board"></textarea>
      </div>
      <div id="mdenrich-status"></div>
      <div style="display:flex;gap:8px">
        <button class="btn-primary" id="mdsave" style="flex:1">Save</button>
        <button class="btn-outline" id="mdenrich" style="flex:1">Enrich with AI</button>
      </div>
      <div id="mdsave-status"></div>
    `;

    document.getElementById('mdrop').addEventListener('click', () => document.getElementById('mdfile').click());
    document.getElementById('mdrop').addEventListener('dragover', e => { e.preventDefault(); });
    document.getElementById('mdrop').addEventListener('drop', e => { e.preventDefault(); handleMsgFile(e.dataTransfer.files[0]); });
    document.getElementById('mdfile').addEventListener('change', e => handleMsgFile(e.target.files[0]));
    document.getElementById('mdtmpl').addEventListener('change', e => {
      const f = applyMeetingTemplate(e.target.value);
      if (f.title) document.getElementById('mdtitle').value = f.title;
      document.getElementById('mddecisions').value = f.decisions;
      document.getElementById('mdactions').value = f.actions;
    });
    document.getElementById('mdtitle').addEventListener('blur', async e => {
      const title = e.target.value.trim();
      if (!title) return;
      const slug = deriveSlug(title);
      if (slug === state.slug) return;
      await loadSeries(slug);
      renderBody();
    });
    body.querySelectorAll('[data-histidx]').forEach(btn => {
      btn.addEventListener('click', () => {
        const i = btn.dataset.histidx;
        const item = document.getElementById(`hist-item-${i}`);
        item.classList.toggle('meeting-history__item--open');
      });
    });
    document.getElementById('mdsave').addEventListener('click', handleSave);
    document.getElementById('mdenrich').addEventListener('click', handleEnrich);
  }

  function renderRequirementsTab() {
    const occurrences = state.series?.occurrences ?? [];
    body.innerHTML = `
      <p class="drawer-label">Scope</p>
      <div class="scope-row" id="mdscope">
        ${occurrences.length
          ? occurrences.map((occ, i) =>
              `<label class="scope-check"><input type="checkbox" data-occidx="${i}" checked> ${occ.date}</label>`
            ).join('')
          : '<span style="font-size:12px;color:var(--muted)">Save a meeting first to generate requirements</span>'
        }
      </div>
      <button class="btn-primary" id="mdgenreq" style="width:100%">Generate Requirements</button>
      <div id="mdreq-output" class="requirements-output" style="${occurrences.length ? '' : 'display:none'}"></div>
      <button class="btn-outline" id="mdreq-save" style="width:100%;display:none">Save to repo</button>
      <div id="mdreq-status"></div>
    `;
    document.getElementById('mdgenreq')?.addEventListener('click', handleGenerateRequirements);
    document.getElementById('mdreq-save')?.addEventListener('click', handleSaveRequirements);
  }

  async function handleMsgFile(file) {
    if (!file?.name.endsWith('.msg')) return;
    const buf = await file.arrayBuffer();
    try {
      const { subject, date, displayTo, displayCc } = parseMsgFile(buf);
      if (subject) {
        const slug = deriveSlug(subject);
        await loadSeries(slug);
        titleEl.textContent = state.series?.frontmatter.title ?? subject;
        badgeEl.textContent = state.series?.occurrences.length
          ? `· ${state.series.occurrences.length} session${state.series.occurrences.length !== 1 ? 's' : ''}`
          : '';
        renderBody();
        if (date) document.getElementById('mddate').value = date.toISOString().slice(0, 10);
        if (subject) document.getElementById('mdtitle').value = state.series?.frontmatter.title ?? subject;
        const att = [displayTo, displayCc].filter(Boolean).join(', ');
        if (att) document.getElementById('mdattendees').value = att;
      }
    } catch {
      document.getElementById('mdenrich-status').innerHTML = '<p style="color:var(--danger);font-size:12px">Could not parse .msg file</p>';
    }
  }

  async function handleEnrich() {
    const btn = document.getElementById('mdenrich');
    const statusEl = document.getElementById('mdenrich-status');
    btn.disabled = true;
    btn.textContent = 'Enriching…';
    statusEl.innerHTML = '';
    try {
      const llm = await pickBackend(config);
      if (!llm) throw new Error('No AI backend — add Anthropic key in Setup.');
      const indexFile = await gh.getFile('INDEX.md');
      let codebaseContent = '';
      if (config.codebaseOwner && config.codebaseRepo) {
        const cbGh = gh.constructor
          ? new gh.constructor({ token: config.token, owner: config.codebaseOwner, repo: config.codebaseRepo })
          : gh;
        const readme = await cbGh.getFile('README.md').catch(() => ({ content: '' }));
        codebaseContent = readme.content ?? '';
      }
      const enriched = await llm.enrich({
        currentNotes: document.getElementById('mdnotes').value,
        seriesContent: state.series?._rawContent ?? '',
        indexContent: indexFile.content ?? '',
        codebaseContent,
      });
      document.getElementById('mdnotes').value = enriched;
      statusEl.innerHTML = '<p style="color:var(--green);font-size:12px">Notes enriched — review before saving</p>';
    } catch (e) {
      statusEl.innerHTML = `<p style="color:var(--danger);font-size:12px">${escHtml(e.message)}</p>`;
    } finally {
      btn.disabled = false;
      btn.textContent = 'Enrich with AI';
    }
  }

  async function handleSave() {
    const btn = document.getElementById('mdsave');
    const statusEl = document.getElementById('mdsave-status');
    const title = document.getElementById('mdtitle').value.trim();
    if (!title) { statusEl.innerHTML = '<p style="color:var(--danger);font-size:12px">Title required</p>'; return; }
    btn.disabled = true;
    btn.textContent = 'Saving…';
    const slug = state.slug ?? deriveSlug(title);
    const occurrence = {
      date: document.getElementById('mddate').value,
      notes: document.getElementById('mdnotes').value,
      decisions: document.getElementById('mddecisions').value,
      actions: document.getElementById('mdactions').value,
      enrichment: '',
    };
    let content;
    if (state.series) {
      content = appendOccurrence(state.series._rawContent, occurrence);
    } else {
      const attendees = document.getElementById('mdattendees').value.trim();
      const template = document.getElementById('mdtmpl').value;
      content = buildSeriesFile({ title, slug, attendees, template }) + formatOccurrence(occurrence);
    }
    try {
      const r = await gh.putContent({ path: `meetings/${slug}.md`, content, message: `feat: meeting notes — ${title} ${occurrence.date}` });
      if (!r.ok) throw new Error(r.message ?? 'save failed');
      statusEl.innerHTML = '<p style="color:var(--green);font-size:12px">Saved</p>';
      await loadSeries(slug);
      renderBody();
    } catch (e) {
      statusEl.innerHTML = `<p style="color:var(--danger);font-size:12px">${escHtml(e.message)}</p>`;
    } finally {
      btn.disabled = false;
      btn.textContent = 'Save';
    }
  }

  async function handleGenerateRequirements() {
    const btn = document.getElementById('mdgenreq');
    const outputEl = document.getElementById('mdreq-output');
    const statusEl = document.getElementById('mdreq-status');
    btn.disabled = true;
    btn.textContent = 'Generating…';
    outputEl.style.display = '';
    outputEl.textContent = 'Working…';
    statusEl.innerHTML = '';
    try {
      const llm = await pickBackend(config);
      if (!llm) throw new Error('No AI backend — add Anthropic key in Setup.');
      const selected = [...document.querySelectorAll('#mdscope input[type=checkbox]:checked')]
        .map(cb => state.series.occurrences[parseInt(cb.dataset.occidx)]);
      if (!selected.length) throw new Error('Select at least one session.');
      const occurrencesContent = selected.map(o =>
        `## ${o.date}\nNotes: ${o.notes}\nDecisions: ${o.decisions}\nActions: ${o.actions}`
      ).join('\n\n');
      const indexFile = await gh.getFile('INDEX.md');
      let codebaseContent = '';
      if (config.codebaseOwner && config.codebaseRepo) {
        const cbGh = new (gh.constructor ?? Object)({ token: config.token, owner: config.codebaseOwner, repo: config.codebaseRepo });
        const readme = await cbGh.getFile('README.md').catch(() => ({ content: '' }));
        codebaseContent = readme.content ?? '';
      }
      const result = await llm.generateRequirements({ occurrencesContent, indexContent: indexFile.content ?? '', codebaseContent });
      outputEl.textContent = result;
      document.getElementById('mdreq-save').style.display = '';
      document.getElementById('mdreq-save').dataset.content = result;
    } catch (e) {
      outputEl.textContent = '';
      statusEl.innerHTML = `<p style="color:var(--danger);font-size:12px">${escHtml(e.message)}</p>`;
    } finally {
      btn.disabled = false;
      btn.textContent = 'Generate Requirements';
    }
  }

  async function handleSaveRequirements() {
    const btn = document.getElementById('mdreq-save');
    const statusEl = document.getElementById('mdreq-status');
    const content = btn.dataset.content;
    if (!content || !state.slug) return;
    btn.disabled = true;
    btn.textContent = 'Saving…';
    const today = TODAY();
    const path = `requirements/${today}-${state.slug}-requirements.md`;
    const header = `# Requirements: ${state.series?.frontmatter.title ?? state.slug} — ${today}\n\n`;
    try {
      const r = await gh.putContent({ path, content: header + content, message: `feat: requirements — ${state.slug} ${today}` });
      if (!r.ok) throw new Error(r.message ?? 'save failed');
      statusEl.innerHTML = `<p style="color:var(--green);font-size:12px">Saved to ${path}</p>`;
    } catch (e) {
      statusEl.innerHTML = `<p style="color:var(--danger);font-size:12px">${escHtml(e.message)}</p>`;
    } finally {
      btn.disabled = false;
      btn.textContent = 'Save to repo';
    }
  }
}

function escHtml(s) {
  return String(s ?? '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/meeting-drawer.js
git commit -m "feat: add meeting drawer component with series history, AI enrichment, requirements generation"
```

---

## Task 7: Update `meetings.js` — series browser

**Files:**
- Modify: `frontend/src/pages/meetings.js`

- [ ] **Step 1: Replace the current form-only `renderMeetings` with a series browser**

Replace the entire `renderMeetings` function (keep `MEETING_TEMPLATES`, `applyMeetingTemplate`, `formatMeeting` exports as-is — they're still used by the drawer):

```js
export function renderMeetings(container, config, gh) {
  container.innerHTML = `
    <div class="card">
      <p class="section-label">Meeting Series</p>
      <div id="mseries-list"><p style="color:var(--muted);font-size:13px">Loading…</p></div>
      <div id="mseries-detail" style="display:none"></div>
    </div>`;

  loadSeriesList(gh, container);
}

async function loadSeriesList(gh, container) {
  const listEl = document.getElementById('mseries-list');
  try {
    const files = await gh.getDir('meetings');
    if (!files.length) {
      listEl.innerHTML = '<p style="color:var(--muted);font-size:13px">No meeting series yet — use the calendar button to add one.</p>';
      return;
    }
    const rows = await Promise.all(
      files.filter(f => f.name.endsWith('.md')).map(async f => {
        const file = await gh.getFile(`meetings/${f.name}`);
        const { frontmatter, occurrences } = parseSeries(file.content);
        return { slug: f.name.replace('.md', ''), frontmatter, occurrences, rawContent: file.content };
      })
    );
    listEl.innerHTML = rows.map(r => `
      <div class="result-item" style="cursor:pointer" data-slug="${escHtml(r.slug)}">
        <div style="display:flex;justify-content:space-between;align-items:baseline">
          <span style="font-weight:600">${escHtml(r.frontmatter.title ?? r.slug)}</span>
          <span style="font-size:11px;color:var(--muted)">${r.occurrences.length} session${r.occurrences.length !== 1 ? 's' : ''}</span>
        </div>
        <div style="font-size:12px;color:var(--muted);margin-top:2px">
          Last: ${r.occurrences[0]?.date ?? 'none'} · Attendees: ${escHtml(r.frontmatter.attendees || '—')}
        </div>
      </div>
    `).join('');
    listEl.querySelectorAll('[data-slug]').forEach(el => {
      el.addEventListener('click', () => {
        const row = rows.find(r => r.slug === el.dataset.slug);
        if (row) renderSeriesDetail(row, gh, container);
      });
    });
  } catch (e) {
    listEl.innerHTML = `<p style="color:var(--danger);font-size:13px">${escHtml(e.message)}</p>`;
  }
}

function renderSeriesDetail(row, gh, container) {
  const detailEl = document.getElementById('mseries-detail');
  const listEl = document.getElementById('mseries-list');
  listEl.style.display = 'none';
  detailEl.style.display = '';
  detailEl.innerHTML = `
    <button id="mback" style="background:none;border:none;color:var(--accent);font-size:13px;cursor:pointer;padding:0 0 12px 0">← All series</button>
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
      <h3 style="font-size:16px">${escHtml(row.frontmatter.title ?? row.slug)}</h3>
      <button class="btn-primary" id="mdetail-genreq" style="font-size:12px;padding:6px 12px">Generate Requirements</button>
    </div>
    <div id="mdetail-req-status"></div>
    ${row.occurrences.map(occ => `
      <div class="result-item" style="margin-bottom:8px">
        <div style="font-weight:600;margin-bottom:6px">${occ.date}</div>
        ${occ.notes ? `<p style="font-size:12px;color:var(--text-2);margin-bottom:4px"><strong>Notes:</strong> ${escHtml(occ.notes)}</p>` : ''}
        ${occ.decisions ? `<p style="font-size:12px;color:var(--text-2);margin-bottom:4px"><strong>Decisions:</strong> ${escHtml(occ.decisions)}</p>` : ''}
        ${occ.actions ? `<p style="font-size:12px;color:var(--text-2)"><strong>Actions:</strong> ${escHtml(occ.actions)}</p>` : ''}
      </div>
    `).join('')}
  `;
  document.getElementById('mback').addEventListener('click', () => {
    detailEl.style.display = 'none';
    listEl.style.display = '';
  });
  document.getElementById('mdetail-genreq').addEventListener('click', () => {
    document.getElementById('mdetail-req-status').innerHTML =
      '<p style="font-size:12px;color:var(--muted)">Open the meeting drawer (calendar button) and use the Requirements tab to generate tickets for this series.</p>';
  });
}

function escHtml(s) {
  return String(s ?? '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}
```

Add the import at the top of the file:

```js
import { parseSeries } from '../services/meetings-series.js';
```

- [ ] **Step 2: Run existing meetings tests to confirm MEETING_TEMPLATES/applyMeetingTemplate/formatMeeting still pass**

```
node --test tests/frontend/meetings.test.js
```
Expected: all pass

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/meetings.js
git commit -m "feat: replace meetings form with series browser"
```

---

## Task 8: Update `setup.js` — codebase repo fields

**Files:**
- Modify: `frontend/src/pages/setup.js`

- [ ] **Step 1: Add codebase repo fields to the setup form**

In `frontend/src/pages/setup.js`, after the Anthropic API key input block, add the codebase repo label and input before the credentials note:

Replace this line in the `root.innerHTML` string:
```
      <p class="muted">Credentials are stored only in this browser's localStorage. Treat this profile as the trust boundary.</p>
```
With:
```
      <label>Codebase repo <span class="muted">(optional — owner/name, used for AI context)</span></label>
      <input id="codebase-repo" type="text" placeholder="myorg/my-service" value="${config.codebaseRepo ? `${config.codebaseOwner ?? ''}/${config.codebaseRepo}` : ''}" />
      <p class="muted">Credentials are stored only in this browser's localStorage. Treat this profile as the trust boundary.</p>
```

In the `#save` onclick handler, update the `next` object to include codebase fields. Replace:
```js
      const next = { token, owner, repo, anthropicKey, username: user.login, check_first: checkFirst };
```
With:
```js
      const codebaseStr = $("#codebase-repo").value.trim();
      const [codebaseOwner, codebaseRepo] = codebaseStr.includes('/') ? codebaseStr.split('/') : ['', ''];
      const next = { token, owner, repo, anthropicKey, username: user.login, check_first: checkFirst, codebaseOwner: codebaseOwner || '', codebaseRepo: codebaseRepo || '' };
```

- [ ] **Step 2: Run all frontend tests**

```
npm test
```
Expected: all pass

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/setup.js
git commit -m "feat: add optional codebase repo field to setup for AI context"
```

---

## Task 9: Wire drawer into `app.js` and `index.html`

**Files:**
- Modify: `frontend/index.html`
- Modify: `frontend/src/app.js`

- [ ] **Step 1: Add drawer mount point to `frontend/index.html`**

In `frontend/index.html`, add `<div id="meeting-drawer-root"></div>` just before the closing `</body>` tag (after the script tag):

Replace:
```html
  <script type="module" src="./src/app.js"></script>
</body>
```
With:
```html
  <script type="module" src="./src/app.js"></script>
  <div id="meeting-drawer-root"></div>
</body>
```

- [ ] **Step 2: Import and init drawer in `frontend/src/app.js`**

Add import at the top of `frontend/src/app.js`:
```js
import { initMeetingDrawer } from './components/meeting-drawer.js';
```

In the `bootstrap` function, after `go("remember")`, add:
```js
  const config = loadConfig();
  if (config.token && config.owner) {
    const gh = new GitHubClient(config);
    initMeetingDrawer(config, gh);
  }
```

- [ ] **Step 3: Run all frontend tests**

```
npm test
```
Expected: all pass

- [ ] **Step 4: Commit**

```bash
git add frontend/index.html frontend/src/app.js
git commit -m "feat: wire meeting drawer into app bootstrap"
```

---

## Task 10: Mirror all changes to `mcp/frontend/`

The `mcp/frontend/` tree is independent (copy-prompts.sh would destroy it). Apply the same changes manually.

- [ ] **Step 1: Copy new service and component files**

```bash
cp frontend/src/services/meetings-series.js mcp/frontend/src/services/meetings-series.js
cp frontend/src/components/meeting-drawer.js mcp/frontend/src/components/meeting-drawer.js
mkdir -p mcp/frontend/src/components
cp frontend/src/components/meeting-drawer.js mcp/frontend/src/components/meeting-drawer.js
```

- [ ] **Step 2: Copy prompt files to mcp/frontend/prompts/**

```bash
cp prompts/enrich-meeting.txt mcp/frontend/prompts/enrich-meeting.txt
cp prompts/generate-requirements.txt mcp/frontend/prompts/generate-requirements.txt
```

- [ ] **Step 3: Apply the `_callText`/`enrich`/`generateRequirements` changes to `mcp/frontend/src/services/llm/anthropic.js`**

The file at `mcp/frontend/src/services/llm/anthropic.js` is identical to `frontend/src/services/llm/anthropic.js`. Copy it:

```bash
cp frontend/src/services/llm/anthropic.js mcp/frontend/src/services/llm/anthropic.js
```

- [ ] **Step 4: Apply the `enrich`/`generateRequirements` stubs to `mcp/frontend/src/services/llm/mcp.js`**

```bash
cp frontend/src/services/llm/mcp.js mcp/frontend/src/services/llm/mcp.js
```

- [ ] **Step 5: Apply `getDir` to `mcp/frontend/src/services/github.js`**

```bash
cp frontend/src/services/github.js mcp/frontend/src/services/github.js
```

- [ ] **Step 6: Apply meetings.js changes to `mcp/frontend/src/pages/meetings.js`**

```bash
cp frontend/src/pages/meetings.js mcp/frontend/src/pages/meetings.js
```

- [ ] **Step 7: Apply setup.js changes to `mcp/frontend/src/pages/setup.js`**

The `mcp/frontend/src/pages/setup.js` differs slightly from `frontend/src/pages/setup.js` (different styling/options). Apply the codebase repo field changes manually in `mcp/frontend/src/pages/setup.js` using the same pattern as Task 8 — add codebase-repo input and update the `next` object in `#save` onclick.

Open `mcp/frontend/src/pages/setup.js` and:

1. Add after the anthropic input inside `root.innerHTML`:
```html
      <label>Codebase repo <span class="muted">(optional — owner/name, used for AI context)</span></label>
      <input id="codebase-repo" type="text" placeholder="myorg/my-service" value="${config.codebaseRepo ? `${config.codebaseOwner ?? ''}/${config.codebaseRepo}` : ''}" />
```

2. In the `#save` onclick, update the `next` object to include `codebaseOwner` and `codebaseRepo` (same as Task 8 Step 1).

- [ ] **Step 8: Apply CSS changes to `mcp/frontend/src/ui/main.css`**

```bash
cp frontend/src/ui/main.css mcp/frontend/src/ui/main.css
```

- [ ] **Step 9: Apply `index.html` changes to `mcp/frontend/index.html`**

Add `<div id="meeting-drawer-root"></div>` before `</body>` in `mcp/frontend/index.html`.

- [ ] **Step 10: Apply `app.js` changes to `mcp/frontend/src/app.js`**

Add the `initMeetingDrawer` import and bootstrap call in `mcp/frontend/src/app.js` (same as Task 9 Step 2).

- [ ] **Step 11: Commit all mcp/frontend changes**

```bash
git add -f mcp/frontend/
git commit -m "feat: mirror meeting PM + requirements changes to mcp/frontend"
```

---

## Task 11: Build verification

- [ ] **Step 1: Run all frontend tests**

```
npm test
```
Expected: all pass

- [ ] **Step 2: Build the Go binary (verifies embed compiles)**

```
cd mcp && go build -o team-memory-mcp-new . && cd ..
```
Expected: builds without errors. If `mcp/prompts/embed.go` errors about missing files, confirm `mcp/prompts/enrich-meeting.txt` and `mcp/prompts/generate-requirements.txt` exist.

- [ ] **Step 3: Run Go tests**

```
cd mcp && go test ./... && cd ..
```
Expected: all pass

- [ ] **Step 4: Clean up build artifact**

```bash
rm mcp/team-memory-mcp-new
```

- [ ] **Step 5: Final commit**

```bash
git add .
git status  # verify nothing unexpected
git commit -m "chore: verify build and tests pass for meeting PM + requirements feature"
```

---

## Self-Review Notes

- `handleEnrich` uses `gh.constructor` to build a second `GitHubClient` for the codebase repo — this depends on `GitHubClient` being a named class (it is). Verify this works or inline the construction.
- `parseSeries` uses regex section splitting — tested for the exact format `formatOccurrence` produces. If format drifts these will desync; the tests guard this.
- `mcp/frontend/src/pages/setup.js` is manually patched in Task 10 Step 7 rather than copied, because it has extra MCP-specific content. Verify the field is added correctly by inspecting the file after editing.
- `meeting-drawer.js` builds a codebase `GitHubClient` with `gh.constructor` — in `mcp/frontend`, `GitHubClient` is imported the same way. This is safe as long as the constructor signature matches (it does — both take `{ token, owner, repo }`).
