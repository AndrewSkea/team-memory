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
  it('strips RE: prefix', () => assert.equal(deriveSlug('RE: Sprint Planning'), 'sprint-planning'));
  it('strips FW: prefix', () => assert.equal(deriveSlug('FW: Sprint Planning'), 'sprint-planning'));
  it('strips FWD: prefix', () => assert.equal(deriveSlug('FWD: Sprint Planning'), 'sprint-planning'));
  it('strips trailing day-date suffix', () => assert.equal(deriveSlug('Sprint Planning - Mon 24 May 2026'), 'sprint-planning'));
  it('strips trailing (2) suffix', () => assert.equal(deriveSlug('Sprint Planning (2)'), 'sprint-planning'));
  it('lowercases and hyphenates', () => assert.equal(deriveSlug('Incident Review'), 'incident-review'));
  it('handles combined prefix + suffix', () => assert.equal(deriveSlug('RE: Sprint Planning - Mon 24 May 2026'), 'sprint-planning'));
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

  it('parses frontmatter title', () => assert.equal(parseSeries(content).frontmatter.title, 'Sprint Planning'));
  it('parses frontmatter slug', () => assert.equal(parseSeries(content).frontmatter.slug, 'sprint-planning'));
  it('parses frontmatter attendees', () => assert.equal(parseSeries(content).frontmatter.attendees, 'Alice, Bob'));
  it('parses 2 occurrences', () => assert.equal(parseSeries(content).occurrences.length, 2));
  it('sorts occurrences newest-first', () => assert.equal(parseSeries(content).occurrences[0].date, '2026-05-24'));
  it('parses occurrence notes', () => assert.equal(parseSeries(content).occurrences[0].notes.trim(), 'New notes'));
  it('parses occurrence decisions', () => assert.equal(parseSeries(content).occurrences[0].decisions.trim(), 'New decision'));
  it('parses occurrence actions', () => assert.equal(parseSeries(content).occurrences[0].actions.trim(), 'Bob: new task'));
  it('returns empty occurrences for frontmatter-only', () => {
    const r = parseSeries('---\ntitle: X\nslug: x\nattendees: \ntemplate: Blank\ncreated: 2026-01-01\n---\n');
    assert.equal(r.occurrences.length, 0);
  });
});

describe('buildSeriesFile', () => {
  it('starts with ---', () => assert.ok(buildSeriesFile({ title: 'T', slug: 't', attendees: '', template: 'Blank' }).startsWith('---\n')));
  it('contains title', () => assert.ok(buildSeriesFile({ title: 'Sprint Planning', slug: 'sprint-planning', attendees: '', template: 'Blank' }).includes('title: Sprint Planning')));
  it('ends with ---', () => assert.ok(buildSeriesFile({ title: 'T', slug: 't', attendees: '', template: 'Blank' }).endsWith('---\n')));
});

describe('formatOccurrence', () => {
  it('contains the date heading', () => assert.ok(formatOccurrence({ date: '2026-05-24', notes: 'n', decisions: 'd', actions: 'a', enrichment: '' }).includes('## 2026-05-24')));
  it('contains notes field', () => assert.ok(formatOccurrence({ date: '2026-05-24', notes: 'my note', decisions: '', actions: '', enrichment: '' }).includes('**Notes:** my note')));
  it('contains enrichment when provided', () => assert.ok(formatOccurrence({ date: '2026-05-24', notes: '', decisions: '', actions: '', enrichment: 'AI says hi' }).includes('**AI Enrichment:** AI says hi')));
});

describe('appendOccurrence', () => {
  it('appends occurrence to content', () => {
    const base = '---\ntitle: T\nslug: t\nattendees: \ntemplate: Blank\ncreated: 2026-01-01\n---\n';
    const result = appendOccurrence(base, { date: '2026-05-24', notes: 'n', decisions: 'd', actions: 'a', enrichment: '' });
    assert.ok(result.includes('## 2026-05-24'));
    assert.ok(result.startsWith('---\n'));
  });
});
