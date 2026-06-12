import { test, expect } from '@playwright/test';
import { login } from './helpers';

async function navigateToFirstFileTimeline(page: import('@playwright/test').Page) {
  // Navigate to files page via sidebar → project
  await page.getByRole('button', { name: 'Projects' }).first().click();
  await page.waitForLoadState('networkidle');
  await page.getByText('changez').first().click();
  await page.waitForLoadState('networkidle');

  // Click first file in the list (any file)
  const fileBtn = page.locator('button span:has-text(".")').first();
  await fileBtn.waitFor({ state: 'visible', timeout: 5000 });
  await fileBtn.click();
  await page.waitForLoadState('networkidle');
}

test.describe('File Timeline', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('shows version timeline for a file', async ({ page }) => {
    await navigateToFirstFileTimeline(page);
    await expect(page.getByText(/v[0-9]+/).first()).toBeVisible({ timeout: 5000 });
  });

  test('shows version jump input on timeline page', async ({ page }) => {
    await navigateToFirstFileTimeline(page);
    await expect(page.getByPlaceholder('Jump to version...')).toBeVisible({ timeout: 5000 });
  });

  test('shows timeline controls on page', async ({ page }) => {
    await navigateToFirstFileTimeline(page);
    // Timeline filters include version jump, source select, action select
    await expect(page.getByPlaceholder('Jump to version...')).toBeVisible({ timeout: 5000 });
    // Verify selects exist
    const selects = page.locator('select');
    await expect(selects.first()).toBeVisible({ timeout: 3000 });
    await expect(selects.nth(1)).toBeVisible({ timeout: 3000 });
  });
});
