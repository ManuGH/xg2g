
import path from 'node:path';
import { defineConfig, devices } from '@playwright/test';

const fixtureServerPort = 3001;
const webuiPort = 4173;
const fixtureServerUrl = `http://127.0.0.1:${fixtureServerPort}`;
const baseURL = `http://127.0.0.1:${webuiPort}`;
const configDir = __dirname;
const fixtureServerDir = path.resolve(configDir, 'fixture-server');
const webuiDir = path.resolve(configDir, '../../frontend/webui');

/**
 * See https://playwright.dev/docs/test-configuration.
 */
export default defineConfig({
  testDir: './specs',
  /* Maximum time one test can run for. */
  timeout: 30 * 1000,
  expect: {
    /**
     * Maximum time expect() should wait for the condition to be met.
     * For example in `await expect(locator).toHaveText();`
     */
    timeout: 5000
  },
  /* Run tests in files in parallel */
  fullyParallel: true,
  /* Fail the build on CI if you accidentally left test.only in the source code. */
  forbidOnly: !!process.env.CI,
  /* Retry on CI only */
  retries: process.env.CI ? 2 : 0,
  /* Opt out of parallel tests on CI. */
  workers: process.env.CI ? 1 : undefined,
  /* Reporter to use. See https://playwright.dev/docs/test-reporters */
  reporter: 'list',
  /* Shared settings for all the projects below. See https://playwright.dev/docs/api/class-testoptions. */
  use: {
    /* Base URL to use in actions like `await page.goto('/')`. */
    baseURL,

    /* Collect trace when retrying the failed test. See https://playwright.dev/docs/trace-viewer */
    trace: 'on-first-retry',
  },

  /* Configure projects for major browsers */
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        locale: 'en-US',
      },
    },
  ],

  /* Run your local dev server before starting the tests */
  webServer: [
    {
      command: 'npm start',
      cwd: fixtureServerDir,
      port: fixtureServerPort,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
    {
      command: `XG2G_WEBUI_BASE=/ XG2G_WEBUI_PROXY_TARGET=${fixtureServerUrl} XG2G_WEBUI_DEV_PORT=${webuiPort} npm run dev -- --host 127.0.0.1 --port ${webuiPort} --strictPort`,
      cwd: webuiDir,
      port: webuiPort,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
  ],
});
