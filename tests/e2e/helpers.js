/**
 * Mock GitHub API responses and seed localStorage with test credentials.
 * Call in beforeEach or at the start of each test that needs auth.
 */

export const TEST_CONFIG = {
  token: 'ghp_test_token_1234567890',
  owner: 'testuser',
  repo: 'test-memory',
  username: 'testuser',
  check_first: false,
};

export const MOCK_INDEX = `# INDEX
GENERAL.md | shared | general
UNSURE.md | shared | unsure
`;

export const MOCK_GENERAL = `# GENERAL

### Entry: 2026-01-15T10:00:00Z | Test pattern
- **Scope**: Team
- **Type**: General
- **Source**: UI
Test memory content.
`;

/** Seed localStorage with test credentials (called in page.addInitScript) */
export function seedConfig(config = TEST_CONFIG) {
  return `localStorage.setItem('team-memory:config', JSON.stringify(${JSON.stringify(config)}))`;
}

/** Wire page.route() to mock all api.github.com calls */
export async function mockGitHubAPI(page, overrides = {}) {
  const defaults = {
    // GET /user
    user: { login: TEST_CONFIG.owner, name: 'Test User' },
    // GET /repos/.../contents/INDEX.md
    indexFile: {
      sha: 'abc123',
      content: Buffer.from(MOCK_INDEX).toString('base64'),
      encoding: 'base64',
    },
    // GET /repos/.../contents/GENERAL.md
    generalFile: {
      sha: 'def456',
      content: Buffer.from(MOCK_GENERAL).toString('base64'),
      encoding: 'base64',
    },
  };
  const mocks = { ...defaults, ...overrides };

  // Playwright last-registered wins — register catch-all first (lowest priority)
  await page.route('https://api.github.com/**', route => {
    if (route.request().method() === 'PUT') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ commit: { sha: 'new123' } }),
      });
    } else {
      route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ message: 'Not Found' }) });
    }
  });

  // Specific routes registered after catch-all take priority
  await page.route('https://api.github.com/repos/**/contents/UNSURE.md', route =>
    route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ message: 'Not Found' }) })
  );

  await page.route('https://api.github.com/repos/**/contents/GENERAL.md', route =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(mocks.generalFile) })
  );

  await page.route('https://api.github.com/repos/**/contents/INDEX.md', route =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(mocks.indexFile) })
  );

  await page.route('https://api.github.com/user', route =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(mocks.user) })
  );
}
