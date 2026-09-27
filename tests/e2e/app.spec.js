// Browser end-to-end tests against the real binary and a fake GitHub.
// Tests run in order and share one install: setup → save → search → review…
import { test, expect } from '@playwright/test';
import { writeFileSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { E2E_DIR } from './paths.mjs';

test.describe.configure({ mode: 'serial' });

// Fail any test that triggers a JS error or an alert (e.g. XSS).
test.beforeEach(async ({ page }) => {
  page.on('pageerror', err => { throw err; });
  page.on('dialog', async d => {
    if (d.type() === 'alert') throw new Error('unexpected alert: ' + d.message());
  });
});

test('serves the app and health', async ({ page, request }) => {
  const h = await (await request.get('/health')).json();
  expect(h.status).toBe('ok');
  expect(h.configured).toBe(false);
  await page.goto('/');
  await expect(page).toHaveTitle('Team Memory');
  expect((await request.get('/src/app.js')).headers()['content-type']).toContain('javascript');
  expect((await request.get('/nope.xyz')).status()).toBe(404);
});

test('first run shows setup and validates input', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Set up team memory' })).toBeVisible();
  await expect(page.locator('#nav')).toBeHidden();
  await page.getByRole('button', { name: 'Connect' }).click();
  await expect(page.locator('#status')).toContainText('owner/name');
  await page.fill('#repo', 'acme/kb');
  await page.fill('#pat', 'wrong-token');
  await page.getByRole('button', { name: 'Connect' }).click();
  await expect(page.locator('#status')).toContainText('invalid or expired', { timeout: 10_000 });
});

test('setup connects, picks a mode, and never exposes the token', async ({ page, request }) => {
  await page.goto('/');
  await page.fill('#repo', 'acme/kb');
  await page.fill('#pat', 'tok');
  await page.getByLabel(/Review everything/).check();
  await page.getByRole('button', { name: 'Connect' }).click();
  await expect(page.locator('#nav')).toBeVisible({ timeout: 10_000 });
  await expect(page.locator('#text')).toBeVisible();
  await expect(page.locator('#footer')).toContainText('acme/kb');
  await expect(page.locator('#footer')).toContainText('review');
  const settings = await (await request.get('/v1/settings')).text();
  expect(settings).not.toContain('"tok"');
  // Token never lands in browser storage.
  const storage = await page.evaluate(() => JSON.stringify(localStorage));
  expect(storage).not.toContain('tok');
});

test('remember saves a note, even without an AI, and strips secrets', async ({ page }) => {
  await page.goto('/#remember');
  await page.fill('#text', 'The payments API rate-limits at 100 rps per key. Retry 429s with jitter. token ghp_abcdefghijklmnopqrstuvwxyz0123456789');
  await page.fill('#about', 'payments-api');
  await page.getByRole('button', { name: 'Save to team memory' }).click();
  await expect(page.locator('#saved')).toContainText('The payments API rate-limits at 100 rps per key');
  await expect(page.locator('#saved')).toContainText('payments-api');
  await expect(page.locator('.toast.error').first()).toBeVisible(); // "AI classification unavailable" / "secrets redacted"
  await expect(page.locator('#text')).toHaveValue('');
});

test('structured types: decision', async ({ page }) => {
  await page.goto('/#remember');
  await page.selectOption('#type', 'decision');
  await expect(page.locator('#section-decision')).toBeVisible();
  await expect(page.locator('#section-general')).toBeHidden();
  await page.fill('#dtitle', 'Use Postgres for the ledger');
  await page.fill('#ddecision', 'We will use Postgres.');
  await page.getByRole('button', { name: 'Save to team memory' }).click();
  await expect(page.locator('#saved')).toContainText('Use Postgres for the ledger');
});

test('HTML in memories is rendered as text', async ({ page }) => {
  await page.goto('/#remember');
  await page.fill('#text', '<img src=x onerror=alert(1)> escaping check');
  await page.getByRole('button', { name: 'Save as written' }).click();
  await expect(page.locator('#saved')).toContainText('<img src=x onerror=alert(1)>');
  await page.goto('/#search');
  await page.fill('#q', 'escaping check');
  await expect(page.locator('.memory').first()).toContainText('onerror');
});

test('search finds by words and by system, and forget hides it', async ({ page }) => {
  await page.goto('/#search');
  await page.fill('#q', 'payments rate limit');
  await expect(page.locator('.memory').first()).toContainText('rate-limits at 100 rps');
  const card = page.locator('.memory').first();
  await expect(card).toContainText('by alice');
  await expect(card.getByRole('link', { name: 'View on GitHub' })).toHaveAttribute('href', /github\.com\/acme\/kb\/blob\/HEAD\/memories\//);
  await page.fill('#q', '');
  await page.locator('.entity-bar').getByRole('button', { name: 'payments-api' }).click();
  await expect(page.locator('.list-heading')).toContainText('about payments-api');
  await expect(page.locator('.memory')).toHaveCount(1);

  page.once('dialog', d => d.accept('Superseded by the new gateway'));
  await page.locator('.memory').first().getByRole('button', { name: 'Forget' }).click();
  await expect(page.locator('.empty')).toBeVisible();
  await page.getByLabel('Include retracted/superseded').check();
  await expect(page.locator('.memory.inactive')).toContainText('retracted');
});

test('review queue: badge, edit and share, discard', async ({ page }) => {
  // Simulate two items held by the capture pipeline.
  const dir = join(E2E_DIR, 'data', 'pending', 'acme__kb');
  mkdirSync(dir, { recursive: true });
  const item = (id, title) => ({
    kind: 'memory', reason: 'capture mode is review', queued_at: new Date().toISOString(), body: 'Details here',
    memory: { id, title, type: 'gotcha', summary: 'Held for review', about: ['kafka'], created: new Date().toISOString(), learned_in: 'acme/stream-svc', author: 'alice', scope: 'team' },
  });
  writeFileSync(join(dir, '20260927-120000-aaaaaa.json'), JSON.stringify(item('20260927-120000-aaaaaa', 'Kafka lag alert threshold is 10k')));
  writeFileSync(join(dir, '20260927-120001-bbbbbb.json'), JSON.stringify(item('20260927-120001-bbbbbb', 'Some half-baked idea')));

  await page.goto('/');
  await expect(page.locator('#review-badge')).toHaveText('2');
  await page.getByRole('button', { name: /Review/ }).click();
  await expect(page.getByRole('heading', { name: '2 waiting for review' })).toBeVisible();
  const first = page.locator('article.pending').filter({ hasText: 'Held because' }).first();
  await first.locator('[data-f="title"]').fill('Kafka consumer lag alerts at 10k messages');
  await first.getByRole('button', { name: 'Share with team' }).click();
  await expect(page.locator('.toast').last()).toContainText('Shared');
  await page.locator('article.pending').getByRole('button', { name: 'Discard' }).click();
  await expect(page.getByRole('heading', { name: 'Nothing to review' })).toBeVisible();
  await expect(page.locator('#review-badge')).toBeHidden();

  await page.goto('/#search');
  await page.fill('#q', 'kafka lag');
  await expect(page.locator('.memory').first()).toContainText('Kafka consumer lag alerts at 10k messages');
});

test('capture mode can be changed from settings', async ({ page }) => {
  await page.goto('/#settings');
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();
  await page.getByLabel(/AI decides/).check();
  await expect(page.locator('.toast').last()).toContainText('AI decides');
  await expect(page.locator('#footer')).toContainText('mode: ai');
});

test('meetings and stats', async ({ page }) => {
  await page.goto('/#meetings');
  await page.selectOption('#mtmpl', 'Incident Review');
  await expect(page.locator('#mtitle')).toHaveValue('Incident Review');
  await page.fill('#mattendees', 'Alice, Bob');
  await page.getByRole('button', { name: 'Save meeting notes' }).click();
  await expect(page.locator('.toast').last()).toContainText('Meeting notes saved');

  await page.goto('/#stats');
  await expect(page.locator('.stat-cell').first()).toContainText('Active memories');
  await expect(page.locator('#stats-body')).toContainText('Contributors');
  await expect(page.locator('#stats-body')).toContainText('alice');
});

test('works on a phone-sized screen without horizontal scroll', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  for (const p of ['remember', 'search', 'review', 'settings', 'stats']) {
    await page.goto('/#' + p);
    await page.waitForTimeout(150);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow, p).toBeLessThanOrEqual(0);
  }
});
