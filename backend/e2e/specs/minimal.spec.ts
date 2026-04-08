
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
    await page.goto('/dashboard');

    await expect(page.getByTestId('auth-surface')).toBeVisible();
    await page.getByTestId('auth-token-input').fill('smoke-token');
    await page.getByTestId('auth-submit').click();

    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(page.getByTestId('dashboard-view')).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Das Erste HD' })).toBeVisible();
    await expect(page.getByText('Control summary')).toBeVisible();
  });
});
