import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { _buildTimeRange, _parseEvents, listTodayMeetings } from '../../frontend/src/services/google-calendar.js';

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

describe('listTodayMeetings', () => {
  it('returns parsed events on success', async () => {
    const fakeFetch = async () => ({
      ok: true,
      status: 200,
      json: async () => ({
        items: [
          { summary: 'Standup', start: { dateTime: '2026-05-25T09:00:00Z' } },
        ],
      }),
    });
    const events = await listTodayMeetings('tok', null, fakeFetch);
    assert.equal(events.length, 1);
    assert.equal(events[0].title, 'Standup');
  });

  it('retries on 401 with refreshed token', async () => {
    let callCount = 0;
    const fakeFetch = async (url, opts) => {
      callCount++;
      if (callCount === 1) return { ok: false, status: 401, json: async () => ({}) };
      return { ok: true, status: 200, json: async () => ({ items: [] }) };
    };
    const events = await listTodayMeetings('old-tok', async () => 'new-tok', fakeFetch);
    assert.equal(callCount, 2);
    assert.deepEqual(events, []);
  });

  it('throws on non-ok response', async () => {
    const fakeFetch = async () => ({ ok: false, status: 500 });
    await assert.rejects(
      () => listTodayMeetings('tok', null, fakeFetch),
      /Calendar API error: 500/
    );
  });
});
