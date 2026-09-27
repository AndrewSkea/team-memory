import { defineConfig } from '@playwright/test';
import { PORT } from './tests/e2e/paths.mjs';

export default defineConfig({
  testDir: './tests/e2e',
  timeout: 30_000,
  retries: process.env.CI ? 1 : 0,
  // The suite walks one shared, stateful install (setup → save → review…).
  workers: 1,
  fullyParallel: false,

  webServer: {
    command: 'node tests/e2e/start-servers.mjs',
    url: `http://127.0.0.1:${PORT}/health`,
    reuseExistingServer: false,
    timeout: 20_000,
    stdout: 'ignore',
    stderr: 'pipe',
  },

  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    headless: true,
  },

  reporter: [['list'], ['html', { open: 'never' }]],
});
