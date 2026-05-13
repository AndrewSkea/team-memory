import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { formatMeeting, MEETING_TEMPLATES, applyMeetingTemplate } from '../../frontend/src/pages/meetings.js';

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

describe('MEETING_TEMPLATES', () => {
  it('has Blank as first entry', () => {
    assert.equal(MEETING_TEMPLATES[0].name, 'Blank');
  });
  it('Sprint Planning template fills title', () => {
    const t = MEETING_TEMPLATES.find(t => t.name === 'Sprint Planning');
    assert.ok(t);
    assert.equal(t.fill.title, 'Sprint Planning');
  });
});

describe('applyMeetingTemplate', () => {
  it('returns fill values from named template', () => {
    const r = applyMeetingTemplate('1:1');
    assert.equal(r.title, '1:1 Check-in');
  });
  it('returns empty fill for Blank', () => {
    const r = applyMeetingTemplate('Blank');
    assert.equal(r.title, '');
  });
  it('returns empty fill for unknown name', () => {
    const r = applyMeetingTemplate('unknown');
    assert.equal(r.title, '');
  });
});
