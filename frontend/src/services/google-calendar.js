const CALENDAR_API = 'https://www.googleapis.com/calendar/v3';

export function _buildTimeRange(now = new Date()) {
  const dateStr = now.toISOString().slice(0, 10);
  return {
    timeMin: `${dateStr}T00:00:00.000Z`,
    timeMax: `${dateStr}T23:59:59.999Z`,
  };
}

export function _parseEvents(items = []) {
  return items.map(e => ({
    title: e.summary || '(no title)',
    start: e.start?.dateTime || e.start?.date || '',
    attendees: (e.attendees || []).map(a => a.email),
  }));
}

export async function listTodayMeetings(accessToken, refreshTokenFn, fetchFn = fetch) {
  const { timeMin, timeMax } = _buildTimeRange();
  const params = new URLSearchParams({
    timeMin,
    timeMax,
    singleEvents: 'true',
    orderBy: 'startTime',
    maxResults: '10',
  });

  let res = await fetchFn(`${CALENDAR_API}/calendars/primary/events?${params}`, {
    headers: { Authorization: `Bearer ${accessToken}` },
  });

  if (res.status === 401 && refreshTokenFn) {
    const newToken = await refreshTokenFn();
    if (newToken) {
      res = await fetchFn(`${CALENDAR_API}/calendars/primary/events?${params}`, {
        headers: { Authorization: `Bearer ${newToken}` },
      });
    }
  }

  if (!res.ok) throw new Error(`Calendar API error: ${res.status}`);
  const data = await res.json();
  return _parseEvents(data.items || []);
}
