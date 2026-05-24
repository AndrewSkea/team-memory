# Meeting PM & Requirements Generation — Design Spec
**Date:** 2026-05-24

## Overview

Extend team-memory into a project management and meeting-notes-to-requirements tool. Adds a floating meeting drawer (accessible from any page), per-series meeting history backed by per-file markdown in GitHub, AI enrichment of notes, and AI-generated JIRA-style requirements saved to the repo.

---

## 1. Data Model

### Meeting series files
Each recurring meeting series lives in a dedicated file:

```
meetings/
  sprint-planning.md
  weekly-standup.md
  incident-review.md
```

File format:
```markdown
---
title: Sprint Planning
slug: sprint-planning
attendees: Alice, Bob, Charlie
template: Sprint Planning
created: 2026-01-10
---

## 2026-05-24
**Notes:** ...
**Decisions:** ...
**Action Items:** ...
**AI Enrichment:** ...

## 2026-05-10
...
```

### Slug derivation
Derived from `.msg` subject or manual title:
- Strip prefixes: `RE:`, `FW:`, `FWD:`
- Strip trailing date/number patterns: `(2)`, `- Mon 24 May`, etc.
- Lowercase + hyphenate
- Example: `"RE: Sprint Planning - Mon 24 May 2026"` → `sprint-planning`

Same slug → same series file. New `.msg` drop matching existing slug = new occurrence appended.

### Requirements files
```
requirements/
  2026-05-24-sprint-planning-requirements.md
  2026-05-10-incident-review-requirements.md
```

Each file contains JIRA-style tickets in markdown:
```markdown
# Requirements: Sprint Planning — 2026-05-24

## TICKET-001: [Title]
**Priority:** P1
**Story Points:** 3
**Labels:** backend, auth
**Description:** ...
**Acceptance Criteria:**
- [ ] ...
- [ ] ...
```

---

## 2. UI Architecture

### Floating meeting button
- Fixed position, bottom-right corner, every page
- Calendar icon, always visible
- Click → right-side drawer slides in (~400px), does not reflow page content

### Drawer states

**Empty / New series**
- Drag-drop zone for `.msg` at top
- Manual title input fallback
- Template selector (existing templates reused)
- Date, attendees, notes textarea, decisions, action items
- "Enrich with AI" button
- "Save" button

**Existing series loaded** (`.msg` subject matches known slug)
- Header badge: "Sprint Planning · 8 sessions"
- Collapsed accordion of past occurrences above form (click to expand)
- Current occurrence form below
- "Enrich with AI" button (uses series history as context)
- "Save" button

**Requirements tab** (tab toggle within drawer)
- Scope selector: this occurrence / all occurrences / selected occurrences (checkboxes)
- "Generate Requirements" button
- Streamed ticket output
- "Save to repo" button

### Meetings nav page (redesigned)
Becomes a series browser:
- Lists all `meetings/*.md` files fetched from GitHub
- Each row: series title, occurrence count, last meeting date
- Click → opens full series history in main content area
- "Generate Requirements" button on each series

---

## 3. AI Enrichment Pipeline

### "Enrich Notes" (drawer button)
Context assembled in order:
1. Current notes (typed content)
2. Past occurrences from series file
3. `INDEX.md` + relevant decisions from team-memory repo
4. If codebase repo configured: `README.md` + files matching topics mentioned in notes (GitHub search API)
5. Available MCP tools called for live context

Output: enriched notes streamed back inline. User reviews and edits before saving.

System prompt: `frontend/prompts/enrich-meeting.txt`

### "Generate Requirements" (requirements tab)
Same context stack. Prompt instructs Claude to output structured JIRA tickets.

Each ticket fields:
- Title
- Description
- Acceptance criteria (bulleted)
- Story points: 1 / 2 / 3 / 5 / 8
- Labels (derived from content)
- Priority: P0 / P1 / P2 / P3

Output streamed into panel as markdown. "Save to repo" commits to `requirements/YYYY-MM-DD-<slug>-requirements.md`.

System prompt: `frontend/prompts/generate-requirements.txt`

---

## 4. Configuration

New field in Setup page: **Codebase repo** (optional). Owner/repo pointer separate from the memory repo. Used by AI context assembly to fetch source files via existing `GitHubClient`.

Stored in `localStorage` under existing `team-memory:config` key as `codebaseOwner` / `codebaseRepo`.

---

## 5. Implementation Scope

### New files
| File | Purpose |
|------|---------|
| `frontend/src/components/meeting-drawer.js` | Floating button + drawer shell, state machine |
| `frontend/src/services/meetings-series.js` | Slug derivation, series file read/write, occurrence parsing |
| `frontend/src/services/requirements-gen.js` | Context assembly, AI call, ticket formatter |
| `frontend/prompts/enrich-meeting.txt` | System prompt for note enrichment |
| `frontend/prompts/generate-requirements.txt` | System prompt for requirements generation |

### Modified files
| File | Change |
|------|--------|
| `frontend/index.html` | Add drawer mount point + floating button |
| `frontend/src/app.js` | Initialise drawer on every page load |
| `frontend/src/pages/meetings.js` | Replace form with series browser |
| `frontend/src/pages/setup.js` | Add codebase repo config fields |
| `mcp/frontend/src/*` | Mirror all changes |

### Out of scope (phase 1)
- Direct JIRA API push
- Real-time AI suggestions while typing
- Calendar integration beyond `.msg` drag-drop

---

## 6. Key Invariants

- No Go backend changes — all new functionality routes through existing `GitHubClient` and `AnthropicDirect`
- Old `MEETINGS.md` entries remain intact — no migration required
- Drawer works on all existing pages without layout changes
- AI calls are always user-initiated (button press) — no auto-fire
