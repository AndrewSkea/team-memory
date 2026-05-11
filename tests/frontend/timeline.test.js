import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { extractEntries, sortByDate } from '../../frontend/src/pages/timeline.js';

describe('extractEntries', () => {
  it('extracts entries from file content', () => {
    const content = `### Entry: 2026-05-06T10:00:00.000Z — Fix auth bug
**Scope:** backend
**Type:** General
**Tags:** auth;bug
**Source:** UI
**Summary:** Fixed timeout issue.
**Bullets:** - root cause was stale token
**Full:**

### Entry: 2026-05-07T09:00:00.000Z — Setup CI
**Scope:** general
**Type:** General
**Tags:** ci
**Source:** UI
**Summary:** Added GitHub Actions.
**Bullets:** - runs on push to master
**Full:**
`;
    const entries = extractEntries(content, 'GENERAL.md');
    assert.equal(entries.length, 2);
    assert.equal(entries[0].title, 'Fix auth bug');
    assert.equal(entries[0].date, '2026-05-06');
    assert.equal(entries[0].source, 'GENERAL.md');
    assert.equal(entries[1].title, 'Setup CI');
  });

  it('returns empty array for content with no entries', () => {
    assert.deepEqual(extractEntries('# Just a header', 'GENERAL.md'), []);
  });
});

describe('sortByDate', () => {
  it('sorts newest first', () => {
    const entries = [
      { date: '2026-04-01' },
      { date: '2026-05-07' },
      { date: '2026-03-15' },
    ];
    const sorted = sortByDate(entries);
    assert.equal(sorted[0].date, '2026-05-07');
    assert.equal(sorted[2].date, '2026-03-15');
  });
});
