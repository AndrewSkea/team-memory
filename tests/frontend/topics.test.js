import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { slugify, formatTopic } from '../../frontend/src/pages/topics.js';

describe('slugify', () => {
  it('lowercases and hyphenates', () => {
    assert.equal(slugify('React Query'), 'react-query');
    assert.equal(slugify('  JWT Auth  '), 'jwt-auth');
    assert.equal(slugify('Go channels & goroutines'), 'go-channels-goroutines');
  });
});

describe('formatTopic', () => {
  it('wraps content with title and date', () => {
    const result = formatTopic({ name: 'React Query', date: '2026-05-07', content: 'Use staleTime wisely.' });
    assert.ok(result.includes('# React Query — Knowledge Dump'));
    assert.ok(result.includes('*Last updated: 2026-05-07*'));
    assert.ok(result.includes('Use staleTime wisely.'));
  });
});
