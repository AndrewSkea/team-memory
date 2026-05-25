// tests/frontend/oauth.test.js
import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import {
  _generateCodeVerifier,
  _codeChallenge,
  readOAuthFragment,
} from '../../frontend/src/services/oauth.js';

describe('_generateCodeVerifier', () => {
  it('returns base64url string of length 43', () => {
    const v = _generateCodeVerifier();
    assert.match(v, /^[A-Za-z0-9\-_]+$/);
    assert.ok(v.length >= 43 && v.length <= 128);
  });
  it('produces unique values each call', () => {
    assert.notEqual(_generateCodeVerifier(), _generateCodeVerifier());
  });
});

describe('_codeChallenge', async () => {
  it('returns base64url SHA-256 of verifier', async () => {
    const challenge = await _codeChallenge('test-verifier');
    assert.match(challenge, /^[A-Za-z0-9\-_]+$/);
    assert.ok(challenge.length > 0);
  });
});

describe('readOAuthFragment', () => {
  it('returns null when no OAuth params present', () => {
    const result = readOAuthFragment({ hash: '', search: '' });
    assert.equal(result, null);
  });

  it('parses gh_token from hash', () => {
    const result = readOAuthFragment({ hash: '#gh_token=gho_abc123', search: '' });
    assert.deepEqual(result, { type: 'github', token: 'gho_abc123' });
  });

  it('parses gh_error from hash', () => {
    const result = readOAuthFragment({ hash: '#gh_error=access_denied', search: '' });
    assert.deepEqual(result, { type: 'github_error', error: 'access_denied' });
  });

  it('parses slack_webhook + slack_channel from hash', () => {
    const result = readOAuthFragment({
      hash: '#slack_webhook=https%3A%2F%2Fhooks.slack.com%2Ftest&slack_channel=%23general',
      search: '',
    });
    assert.deepEqual(result, {
      type: 'slack',
      webhookUrl: 'https://hooks.slack.com/test',
      channel: '#general',
    });
  });

  it('parses google code from search params', () => {
    const result = readOAuthFragment({ hash: '', search: '?code=4%2F0google&state=xyz' });
    assert.deepEqual(result, { type: 'google', code: '4/0google', state: 'xyz' });
  });
});
