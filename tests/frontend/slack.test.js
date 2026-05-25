import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { postToSlack } from '../../frontend/src/services/slack.js';

describe('postToSlack', () => {
  it('POSTs JSON body to webhookUrl', async () => {
    let captured;
    const fakeFetch = async (url, opts) => {
      captured = { url, opts };
      return { ok: true, status: 200 };
    };
    await postToSlack('https://hooks.slack.com/test', 'Hello team', fakeFetch);
    assert.equal(captured.url, 'https://hooks.slack.com/test');
    assert.equal(captured.opts.method, 'POST');
    const body = JSON.parse(captured.opts.body);
    assert.equal(body.text, 'Hello team');
  });

  it('throws on non-ok response', async () => {
    const fakeFetch = async () => ({ ok: false, status: 400 });
    await assert.rejects(
      () => postToSlack('https://hooks.slack.com/test', 'hello', fakeFetch),
      /400/
    );
  });
});
