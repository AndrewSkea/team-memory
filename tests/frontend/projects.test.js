import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { formatProject } from '../../frontend/src/pages/projects.js';

describe('formatProject', () => {
  it('formats project with milestones', () => {
    const result = formatProject({
      name: 'Redesign Auth',
      status: 'Active',
      goal: 'Replace tokens with JWTs.',
      milestones: 'Design schema\n[x] Audit existing code',
      notes: 'Legal flagged this.',
    });
    assert.ok(result.includes('### Project: Redesign Auth'));
    assert.ok(result.includes('**Status:** Active'));
    assert.ok(result.includes('**Goal:** Replace tokens with JWTs.'));
    assert.ok(result.includes('- [ ] Design schema'));
    assert.ok(result.includes('- [x] Audit existing code'));
    assert.ok(result.includes('Legal flagged this.'));
    assert.ok(result.endsWith('---\n'));
  });

  it('omits notes section when empty', () => {
    const result = formatProject({ name: 'X', status: 'Active', goal: 'G', milestones: 'M1', notes: '' });
    assert.ok(!result.includes('**Notes:**'));
  });
});
