import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { formatMeeting } from '../../frontend/src/pages/meetings.js';

describe('formatMeeting', () => {
  it('formats a meeting with all fields', () => {
    const result = formatMeeting({
      title: 'Sprint Planning',
      date: '2026-05-07',
      attendees: 'Alice, Bob',
      decisions: 'Move to 2-week sprints\nUse GitHub Projects',
      actionItems: 'Alice: set up project\nBob: draft template',
    });
    assert.ok(result.includes('### Meeting: Sprint Planning'));
    assert.ok(result.includes('**Date:** 2026-05-07'));
    assert.ok(result.includes('**Attendees:** Alice, Bob'));
    assert.ok(result.includes('- Move to 2-week sprints'));
    assert.ok(result.includes('- [ ] Alice: set up project'));
    assert.ok(result.endsWith('---\n'));
  });

  it('omits empty sections', () => {
    const result = formatMeeting({
      title: 'Quick sync',
      date: '2026-05-07',
      attendees: '',
      decisions: '',
      actionItems: '',
    });
    assert.ok(!result.includes('**Attendees:**'));
    assert.ok(!result.includes('**Decisions:**'));
    assert.ok(!result.includes('**Action Items:**'));
  });
});
