import { test, expect } from '@playwright/test';
import { login } from './helpers';

test.describe('Trends Analysis', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('shows trends page with stat cards', async ({ page }) => {
    await page.goto('/projects/changez/trends');
    await page.waitForLoadState('networkidle');

    await expect(page.getByText('Total Changes')).toBeVisible({ timeout: 5000 });
    await expect(page.getByText('Files Affected')).toBeVisible({ timeout: 5000 });
    await expect(page.getByText('Period')).toBeVisible({ timeout: 5000 });
  });

  test('shows date range selector with quick options', async ({ page }) => {
    await page.goto('/projects/changez/trends');
    await page.waitForLoadState('networkidle');

    await expect(page.getByText('Last 7 days')).toBeVisible({ timeout: 5000 });
    await expect(page.getByText('Last 30 days')).toBeVisible({ timeout: 5000 });
    await expect(page.getByText('Custom')).toBeVisible({ timeout: 5000 });
  });

  test('shows source distribution section', async ({ page }) => {
    await page.goto('/projects/changez/trends');
    await page.waitForLoadState('networkidle');

    await expect(page.getByText('Source Distribution')).toBeVisible({ timeout: 5000 });
  });

  test('shows top changed files section', async ({ page }) => {
    await page.goto('/projects/changez/trends');
    await page.waitForLoadState('networkidle');

    await expect(page.getByText('Top Changed Files')).toBeVisible({ timeout: 5000 });
  });

  test('can navigate back to files page', async ({ page }) => {
    await page.goto('/projects/changez/trends');
    await page.waitForLoadState('networkidle');

    // Click the "← Back" button
    await page.getByRole('button', { name: /Back/ }).click();
    await page.waitForLoadState('networkidle');

    // Should navigate to files page for this project (breadcrumb shows "Files")
    await expect(page.getByText('Files', { exact: true })).toBeVisible({ timeout: 5000 });
  });
});
