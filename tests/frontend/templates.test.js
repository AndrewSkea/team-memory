import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { TEMPLATES, getTemplate } from '../../frontend/src/services/templates.js';

describe('TEMPLATES', () => {
  it('has at least 6 entries', () => assert.ok(TEMPLATES.length >= 6));
  it('first template is Blank with empty text', () => {
    assert.equal(TEMPLATES[0].name, 'Blank');
    assert.equal(TEMPLATES[0].text, '');
  });
  it('every template has name and text properties', () => {
    TEMPLATES.forEach(t => {
      assert.ok(typeof t.name === 'string');
      assert.ok(typeof t.text === 'string');
    });
  });
});

describe('getTemplate', () => {
  it('returns template by name', () => {
    const t = getTemplate('Bug report');
    assert.ok(t.text.includes('Bug:'));
  });
  it('returns blank for unknown name', () => {
    assert.equal(getTemplate('unknown').text, '');
  });
});
