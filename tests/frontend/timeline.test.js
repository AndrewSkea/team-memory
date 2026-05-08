import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { extractEntries, sortByDate } from '../../frontend/src/pages/timeline.js';

describe('extractEntries', () => {
  it('extracts entries from file content', () => {
    const content = `### Entry: Fix auth bug
**Date:** 2026-05-06
**Tags:** auth;bug
**Summary:** Fixed timeout issue.
- root cause was stale token

### Entry: Setup CI
**Date:** 2026-05-07
**Tags:** ci
**Summary:** Added GitHub Actions.
- runs on push to master
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
