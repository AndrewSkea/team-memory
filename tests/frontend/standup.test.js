import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { formatStandup } from '../../frontend/src/pages/standup.js';

describe('formatStandup', () => {
  it('formats full standup', () => {
    const result = formatStandup({
      date: '2026-05-07',
      yesterday: 'Fixed auth bug\nReviewed PRs',
      today: 'Start feature X',
      blockers: 'Waiting on design',
    });
    assert.ok(result.includes('### Standup: 2026-05-07'));
    assert.ok(result.includes('**Yesterday:**'));
    assert.ok(result.includes('- Fixed auth bug'));
    assert.ok(result.includes('**Today:**'));
    assert.ok(result.includes('**Blockers:**'));
    assert.ok(result.endsWith('---\n'));
  });

  it('omits empty blockers section', () => {
    const result = formatStandup({ date: '2026-05-07', yesterday: 'done stuff', today: 'do more', blockers: '' });
    assert.ok(!result.includes('**Blockers:**'));
  });
});
