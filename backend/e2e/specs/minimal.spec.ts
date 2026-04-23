
import { test, expect } from '@playwright/test';

const fixtureServerUrl = 'http://127.0.0.1:3001';

test.describe('WebUI browser smoke', () => {
  test.beforeEach(async ({ request }) => {
    const response = await request.post(`${fixtureServerUrl}/__admin/scenario`, {
      data: { id: 'minimal-boot' }
    });
    expect(response.ok()).toBeTruthy();
  });

  test('authenticates and renders the dashboard with fixture data', async ({ page }) => {
    await page.context().clearCookies();
    await page.addInitScript(() => {
      window.localStorage.clear();
      window.sessionStorage.clear();
    });

    await page.goto('/dashboard');

    const authSurface = page.getByTestId('auth-surface');
    const dashboardView = page.getByTestId('dashboard-view');
    const initialState = await Promise.race([
      authSurface.waitFor({ state: 'visible' }).then(() => 'auth'),
      dashboardView.waitFor({ state: 'visible' }).then(() => 'dashboard'),
    ]);

    if (initialState === 'auth') {
      await page.getByTestId('auth-token-input').fill('smoke-token');
      await Promise.all([
        page.waitForURL(/\/dashboard$/),
        page.getByTestId('auth-token-input').press('Enter'),
      ]);
    }

    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(dashboardView).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Das Erste HD' })).toBeVisible();
    await expect(page.getByText('Control summary')).toBeVisible();
  });
});
