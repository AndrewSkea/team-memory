import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { extractTagsFromIndex, filterByTag } from '../../frontend/src/pages/lookup.js';

describe('extractTagsFromIndex', () => {
  it('extracts unique tags from index content', () => {
    const index = `## GENERAL.md
### Entry: Fix auth
**Tags:** auth;bug;backend
### Entry: Add CI
**Tags:** ci;backend
`;
    const tags = extractTagsFromIndex(index);
    assert.ok(tags.includes('auth'));
    assert.ok(tags.includes('bug'));
    assert.ok(tags.includes('ci'));
    assert.ok(tags.includes('backend'));
    assert.equal(tags.filter(t => t === 'backend').length, 1, 'no duplicates');
  });
});

describe('filterByTag', () => {
  it('returns entries matching tag', () => {
    const entries = [
      { title: 'Fix auth', tags: ['auth', 'bug'] },
      { title: 'Add CI', tags: ['ci'] },
    ];
    const result = filterByTag(entries, 'auth');
    assert.equal(result.length, 1);
    assert.equal(result[0].title, 'Fix auth');
  });

  it('returns all entries for null tag', () => {
    const entries = [{ tags: ['a'] }, { tags: ['b'] }];
    assert.equal(filterByTag(entries, null).length, 2);
  });
});
