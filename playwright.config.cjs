const { defineConfig } = require('@playwright/test');
module.exports = defineConfig({
  testDir: './web/e2e',
  testMatch: '**/*.spec.cjs',
  workers: 1,
  timeout: 90000,
  expect: { timeout: 15000 },
  use: {
    actionTimeout: 15000,
    baseURL: process.env.BROWSER_BASE_URL || 'http://127.0.0.1:19188',
    browserName: 'chromium',
    launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined },
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
});
