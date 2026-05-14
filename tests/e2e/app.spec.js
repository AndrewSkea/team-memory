import { test, expect } from '@playwright/test';
import { seedConfig, mockGitHubAPI, TEST_CONFIG, MOCK_INDEX } from './helpers.js';

// ── server / static assets ────────────────────────────────────────────────────

test('serves index.html with correct title', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveTitle('Team Memory');
  await expect(page.locator('h1')).toContainText('Team Memory');
});

test('serves CSS and JS static files', async ({ page, request }) => {
  const css = await request.get('/src/ui/main.css');
  expect(css.status()).toBe(200);
  expect(css.headers()['content-type']).toContain('text/css');

  const js = await request.get('/src/app.js');
  expect(js.status()).toBe(200);
  expect(js.headers()['content-type']).toContain('javascript');
});

test('health endpoint returns ok', async ({ request }) => {
  const res = await request.get('/health');
  expect(res.status()).toBe(200);
  const body = await res.json();
  expect(body.status).toBe('ok');
});

test('unknown path returns 404', async ({ request }) => {
  const res = await request.get('/this-does-not-exist.xyz');
  expect(res.status()).toBe(404);
});

// ── navigation ────────────────────────────────────────────────────────────────

test('nav renders all four tabs', async ({ page }) => {
  await page.goto('/');
  const nav = page.locator('.nav-card');
  await expect(nav.locator('button', { hasText: 'Remember' })).toBeVisible();
  await expect(nav.locator('button', { hasText: 'Lookup' })).toBeVisible();
  await expect(nav.locator('button', { hasText: 'Stats' })).toBeVisible();
  await expect(nav.locator('button', { hasText: 'Meetings' })).toBeVisible();
});

test('no credentials redirects to setup', async ({ page }) => {
  await page.goto('/');
  // With no localStorage, app should show setup form
  await expect(page.locator('input#pat')).toBeVisible({ timeout: 5000 });
});

// ── setup page ────────────────────────────────────────────────────────────────

test('setup: validates empty fields', async ({ page }) => {
  await page.goto('/');
  await page.locator('#save').click();
  const status = page.locator('#status');
  await expect(status).toContainText('required');
});

test('setup: successful auth saves config and navigates to remember', async ({ page }) => {
  await mockGitHubAPI(page);
  await page.goto('/');

  await page.fill('#pat', TEST_CONFIG.token);
  await page.fill('#repo', `${TEST_CONFIG.owner}/${TEST_CONFIG.repo}`);
  await page.locator('#save').click();

  // Should show authenticated username
  await expect(page.locator('#status')).toContainText('testuser', { timeout: 8000 });

  // Should navigate away from setup
  await expect(page.locator('textarea#text')).toBeVisible({ timeout: 5000 });
});

// ── remember page ─────────────────────────────────────────────────────────────

test('remember: shows user row with owner name', async ({ page }) => {
  await page.addInitScript(seedConfig());
  await mockGitHubAPI(page);
  await page.goto('/');

  await expect(page.locator('.user-name')).toContainText(TEST_CONFIG.owner);
  await expect(page.locator('.user-repo')).toContainText(TEST_CONFIG.repo);
});

test('remember: has TYPE and SCOPE selects', async ({ page }) => {
  await page.addInitScript(seedConfig());
  await mockGitHubAPI(page);
  await page.goto('/');

  await expect(page.locator('select#type')).toBeVisible();
  await expect(page.locator('select#scope')).toBeVisible();
});

test('remember: checking unsure sets type to Unsure', async ({ page }) => {
  await page.addInitScript(seedConfig());
  await mockGitHubAPI(page);
  await page.goto('/');

  await page.locator('#unsure').check();
  await expect(page.locator('select#type')).toHaveValue('Unsure');
});

test('remember: save triggers GitHub commit and shows toast', async ({ page }) => {
  await page.addInitScript(seedConfig());
  await mockGitHubAPI(page);
  await page.goto('/');

  await page.fill('textarea#text', 'This is a test memory entry');
  await page.locator('button#save').click();

  await expect(page.locator('.toast')).toContainText('Saved', { timeout: 8000 });
});

test('remember: save with empty text shows error toast', async ({ page }) => {
  await page.addInitScript(seedConfig());
  await mockGitHubAPI(page);
  await page.goto('/');

  await page.locator('button#save').click();
  await expect(page.locator('.toast.error')).toBeVisible({ timeout: 5000 });
});

test('remember: file attach populates textarea', async ({ page }) => {
  await page.addInitScript(seedConfig());
  await mockGitHubAPI(page);
  await page.goto('/');

  // Set file input to a virtual file
  const fileInput = page.locator('input#file');
  await fileInput.setInputFiles({
    name: 'test.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('Content from file upload'),
  });

  await expect(page.locator('textarea#text')).toHaveValue('Content from file upload');
});

// ── lookup page ───────────────────────────────────────────────────────────────

test('lookup: search input visible on tab click', async ({ page }) => {
  await page.addInitScript(seedConfig());
  await mockGitHubAPI(page);
  await page.goto('/');

  await page.locator('.nav-btn', { hasText: 'Lookup' }).click();
  await expect(page.locator('input.search-input')).toBeVisible();
});

test('lookup: typing shows result sections', async ({ page }) => {
  await page.addInitScript(seedConfig());
  await mockGitHubAPI(page);
  await page.goto('/');

  await page.locator('.nav-btn', { hasText: 'Lookup' }).click();
  await page.locator('input.search-input').fill('general');

  // Wait for debounce + results
  await expect(page.locator('.result-item').first()).toBeVisible({ timeout: 5000 });
  await expect(page.locator('.result-label').first()).toContainText('Files');
});

// ── stats page ────────────────────────────────────────────────────────────────

test('stats: loads stat cells', async ({ page }) => {
  await page.addInitScript(seedConfig());
  await mockGitHubAPI(page);
  await page.goto('/');

  await page.locator('.nav-btn', { hasText: 'Stats' }).click();
  await expect(page.locator('.stat-cell').first()).toBeVisible({ timeout: 8000 });
});

// ── meetings page ────────────────────────────────────────────────────────────

test('meetings: loads without error', async ({ page }) => {
  await page.addInitScript(seedConfig());
  await mockGitHubAPI(page);
  await page.goto('/');

  await page.locator('.nav-btn', { hasText: 'Meetings' }).click();
  await expect(page.locator('#root')).toBeVisible({ timeout: 8000 });
});

// ── forget auth ───────────────────────────────────────────────────────────────

test('forget auth button clears config and shows setup', async ({ page }) => {
  await page.addInitScript(seedConfig());
  await mockGitHubAPI(page);
  await page.goto('/');

  // Click the ✕ button in the user row
  await page.locator('#forget-btn').click();
  page.on('dialog', dialog => dialog.accept());
  await page.locator('#forget-btn').click();

  await expect(page.locator('input#pat')).toBeVisible({ timeout: 5000 });
});

// ── nav active state ──────────────────────────────────────────────────────────

test('active nav tab has correct class', async ({ page }) => {
  await page.addInitScript(seedConfig());
  await mockGitHubAPI(page);
  await page.goto('/');

  // Remember is active by default
  await expect(page.locator('.nav-btn.active')).toContainText('Remember');

  // Click Lookup — it becomes active
  await page.locator('.nav-btn', { hasText: 'Lookup' }).click();
  await expect(page.locator('.nav-btn.active')).toContainText('Lookup');
  await expect(page.locator('.nav-btn', { hasText: 'Remember' })).not.toHaveClass(/active/);
});
