import { defineConfig } from '@playwright/test';

const PORT = 19438;
const binaryPath = process.platform === 'win32'
  ? 'mcp\\team-memory-mcp.exe'
  : './mcp/team-memory-mcp';

export default defineConfig({
  testDir: './tests/e2e',
  timeout: 30_000,
  retries: 1,

  webServer: {
    command: `${binaryPath} --port ${PORT}`,
    url: `http://127.0.0.1:${PORT}/health`,
    reuseExistingServer: !process.env.CI,
    timeout: 10_000,
    stdout: 'ignore',
    stderr: 'ignore',
  },

  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    headless: true,
  },

  reporter: [['list'], ['html', { open: 'never' }]],
});
