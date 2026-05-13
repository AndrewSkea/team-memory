import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { parseReminders, formatReminderEntry, markDone } from '../../frontend/src/pages/reminders.js';

const SAMPLE = `### Submit Q2 report
**Due:** 2026-05-20
**Tags:** finance, deadline
- Review all submissions
- Compile into PDF
---
### ~~Book flights~~ [DONE]
**Due:** 2026-05-10
**Tags:** travel
- Check Expedia
---
`;

describe('parseReminders', () => {
  it('parses two entries', () => {
    const items = parseReminders(SAMPLE);
    assert.equal(items.length, 2);
  });
  it('parses title and due date', () => {
    const items = parseReminders(SAMPLE);
    assert.equal(items[0].title, 'Submit Q2 report');
    assert.equal(items[0].dueDate, '2026-05-20');
  });
  it('parses tags', () => {
    const items = parseReminders(SAMPLE);
    assert.equal(items[0].tags, 'finance, deadline');
  });
  it('parses bullets', () => {
    const items = parseReminders(SAMPLE);
    assert.deepEqual(items[0].bullets, ['Review all submissions', 'Compile into PDF']);
  });
  it('marks done entries', () => {
    const items = parseReminders(SAMPLE);
    assert.equal(items[0].done, false);
    assert.equal(items[1].done, true);
    assert.equal(items[1].title, 'Book flights');
  });
});

describe('formatReminderEntry', () => {
  it('produces correct markdown', () => {
    const md = formatReminderEntry({ short_title: 'Do thing', bullets: ['step 1'], tags: 'work' }, '2026-06-01');
    assert.ok(md.includes('### Do thing'));
    assert.ok(md.includes('**Due:** 2026-06-01'));
    assert.ok(md.includes('**Tags:** work'));
    assert.ok(md.includes('- step 1'));
    assert.ok(md.endsWith('---\n'));
  });
});

describe('markDone', () => {
  it('marks a reminder done in raw markdown', () => {
    const result = markDone(SAMPLE, 'Submit Q2 report');
    assert.ok(result.includes('### ~~Submit Q2 report~~ [DONE]'));
    assert.ok(!result.includes('### Submit Q2 report\n'));
  });
  it('is a no-op for already-done items', () => {
    const result = markDone(SAMPLE, 'Book flights');
    assert.equal(result, SAMPLE);
  });
});
