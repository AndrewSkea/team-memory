import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { AnthropicDirect } from '../../frontend/src/services/llm/anthropic.js';

function makeFetch(text) {
  return async () => ({
    ok: true,
    json: async () => ({ content: [{ type: 'text', text }] }),
    text: async () => text,
  });
}

function makeFailFetch(status) {
  return async () => ({ ok: false, status, text: async () => 'error body' });
}

describe('AnthropicDirect.enrich', () => {
  it('returns enriched text string', async () => {
    const llm = new AnthropicDirect({
      apiKey: 'key',
      loadPrompt: async () => 'sys',
      fetch: makeFetch('enriched notes here'),
    });
    const result = await llm.enrich({ seriesContent: '', currentNotes: 'raw', indexContent: '', codebaseContent: '' });
    assert.equal(typeof result, 'string');
    assert.equal(result, 'enriched notes here');
  });

  it('throws on non-ok response', async () => {
    const llm = new AnthropicDirect({
      apiKey: 'key',
      loadPrompt: async () => 'sys',
      fetch: makeFailFetch(429),
    });
    await assert.rejects(
      () => llm.enrich({ seriesContent: '', currentNotes: '', indexContent: '', codebaseContent: '' }),
      /anthropic 429/
    );
  });
});

describe('AnthropicDirect.generateRequirements', () => {
  it('returns requirements markdown string', async () => {
    const llm = new AnthropicDirect({
      apiKey: 'key',
      loadPrompt: async () => 'sys',
      fetch: makeFetch('## TICKET-001: Do thing\n**Priority:** P1'),
    });
    const result = await llm.generateRequirements({ occurrencesContent: '', indexContent: '', codebaseContent: '' });
    assert.ok(result.includes('TICKET-001'));
  });

  it('uses max_tokens 4096 (not 1024)', async () => {
    let capturedBody;
    const llm = new AnthropicDirect({
      apiKey: 'key',
      loadPrompt: async () => 'sys',
      fetch: async (url, opts) => {
        capturedBody = JSON.parse(opts.body);
        return { ok: true, json: async () => ({ content: [{ type: 'text', text: 'ok' }] }) };
      },
    });
    await llm.generateRequirements({ occurrencesContent: '', indexContent: '', codebaseContent: '' });
    assert.equal(capturedBody.max_tokens, 4096);
  });
});
