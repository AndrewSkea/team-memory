import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { removeEntryFromContent } from '../../frontend/src/pages/stale.js';

describe('removeEntryFromContent', () => {
  it('removes a named entry block from file content', () => {
    const content = `### Entry: Old thing
**Date:** 2025-01-01
**Summary:** Something old.

### Entry: Recent thing
**Date:** 2026-05-01
**Summary:** Something new.
`;
    const result = removeEntryFromContent(content, 'Old thing');
    assert.ok(!result.includes('Old thing'));
    assert.ok(result.includes('Recent thing'));
  });

  it('returns content unchanged if entry not found', () => {
    const content = `### Entry: Existing\n**Date:** 2026-01-01\n`;
    assert.equal(removeEntryFromContent(content, 'Missing'), content);
  });
});
