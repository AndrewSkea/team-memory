import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { formatDecision } from '../../frontend/src/pages/decisions.js';

describe('formatDecision', () => {
  it('formats decision with all fields', () => {
    const result = formatDecision({
      title: 'Use JWT for auth',
      date: '2026-05-07',
      status: 'Accepted',
      context: 'Legal flagged session tokens.',
      decision: 'Switch to JWTs.',
      consequences: 'Clients must handle refresh.\nSession table can be dropped.',
    });
    assert.ok(result.includes('### Decision: Use JWT for auth'));
    assert.ok(result.includes('**Status:** Accepted'));
    assert.ok(result.includes('**Context:**'));
    assert.ok(result.includes('Legal flagged session tokens.'));
    assert.ok(result.includes('**Decision:**'));
    assert.ok(result.includes('**Consequences:**'));
    assert.ok(result.includes('- Clients must handle refresh.'));
    assert.ok(result.endsWith('---\n'));
  });
});
