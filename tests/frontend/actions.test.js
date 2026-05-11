import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { formatAction, toggleActionLine } from '../../frontend/src/pages/actions.js';

describe('formatAction', () => {
  it('formats action with all fields', () => {
    const result = formatAction({ text: 'Fix login bug', priority: 'HIGH', owner: 'Alice', due: '2026-05-14' });
    assert.equal(result, '- [ ] HIGH | Fix login bug | Owner: Alice | Due: 2026-05-14\n');
  });

  it('omits empty owner and due', () => {
    const result = formatAction({ text: 'Write docs', priority: 'LOW', owner: '', due: '' });
    assert.equal(result, '- [ ] LOW | Write docs\n');
  });
});

describe('toggleActionLine', () => {
  it('marks incomplete action as complete', () => {
    const content = '- [ ] HIGH | Fix bug\n- [ ] LOW | Write docs\n';
    const result = toggleActionLine(content, '- [ ] HIGH | Fix bug');
    assert.ok(result.includes('- [x] HIGH | Fix bug'));
    assert.ok(result.includes('- [ ] LOW | Write docs'));
  });

  it('marks complete action as incomplete', () => {
    const content = '- [x] HIGH | Fix bug\n';
    const result = toggleActionLine(content, '- [x] HIGH | Fix bug');
    assert.ok(result.includes('- [ ] HIGH | Fix bug'));
  });
});
