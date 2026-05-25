import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { _buildTimeRange, _parseEvents } from '../../frontend/src/services/google-calendar.js';

describe('_buildTimeRange', () => {
  it('returns timeMin at start of day and timeMax at end of day', () => {
    const { timeMin, timeMax } = _buildTimeRange(new Date('2026-05-25T14:00:00Z'));
    assert.match(timeMin, /^2026-05-25T00:00:00/);
    assert.match(timeMax, /^2026-05-25T23:59:59/);
  });
});

describe('_parseEvents', () => {
  it('extracts title, start, attendees from Google Calendar event', () => {
    const raw = [
      {
        summary: 'Sprint Planning',
        start: { dateTime: '2026-05-25T10:00:00+01:00' },
        attendees: [
          { email: 'alice@example.com' },
          { email: 'bob@example.com' },
        ],
      },
    ];
    const events = _parseEvents(raw);
    assert.equal(events.length, 1);
    assert.equal(events[0].title, 'Sprint Planning');
    assert.equal(events[0].start, '2026-05-25T10:00:00+01:00');
    assert.deepEqual(events[0].attendees, ['alice@example.com', 'bob@example.com']);
  });

  it('handles missing attendees', () => {
    const raw = [{ summary: 'Solo', start: { dateTime: '2026-05-25T09:00:00Z' } }];
    const events = _parseEvents(raw);
    assert.deepEqual(events[0].attendees, []);
  });

  it('falls back to start.date for all-day events', () => {
    const raw = [{ summary: 'Holiday', start: { date: '2026-05-25' } }];
    const events = _parseEvents(raw);
    assert.equal(events[0].start, '2026-05-25');
  });
});
