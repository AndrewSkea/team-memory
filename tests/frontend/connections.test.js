import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { _providerStatus } from '../../frontend/src/pages/connections.js';

describe('_providerStatus', () => {
  it('returns connected label for github with token', () => {
    const s = _providerStatus('github', { githubOAuthToken: 'tok', username: 'alice' });
    assert.ok(s.connected);
    assert.match(s.label, /alice/);
  });

  it('returns not-connected for github without token', () => {
    const s = _providerStatus('github', {});
    assert.ok(!s.connected);
  });

  it('returns connected for google with accessToken', () => {
    const s = _providerStatus('google', { googleAccessToken: 'tok' });
    assert.ok(s.connected);
  });

  it('returns connected for slack with webhookUrl', () => {
    const s = _providerStatus('slack', { slackWebhookUrl: 'https://hooks.slack.com/x', slackChannel: '#general' });
    assert.ok(s.connected);
    assert.match(s.label, /#general/);
  });

  it('returns not-connected for slack without webhookUrl', () => {
    const s = _providerStatus('slack', {});
    assert.ok(!s.connected);
  });
});
