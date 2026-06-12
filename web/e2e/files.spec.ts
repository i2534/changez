import { test, expect } from '@playwright/test';
import { login } from './helpers';

test.describe('Files', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('shows file list for a project', async ({ page }) => {
    // Navigate to changez project files
    await page.goto('/projects/changez/files');
    await page.waitForLoadState('networkidle');

    // Should see the files search input
    await expect(page.getByPlaceholder('Search files...')).toBeVisible({ timeout: 5000 });
  });

  test('shows dashboard breadcrumb on home page', async ({ page }) => {
    // Already at '/' after login in beforeEach
    await expect(page.getByText('Dashboard')).toBeVisible({ timeout: 5000 });
  });

  test('shows project name breadcrumb when on files page', async ({ page }) => {
    await page.goto('/projects/changez/files');
    await page.waitForLoadState('networkidle');

    // Breadcrumb nav shows the project name as a button
    await expect(page.locator('nav button:has-text("changez")').first()).toBeVisible({ timeout: 5000 });
  });

  test('shows breadcrumb navigation back to dashboard', async ({ page }) => {
    await page.goto('/projects/changez/files');
    await page.waitForLoadState('networkidle');

    // Click the Changez logo (navigates to dashboard)
    await page.getByRole('button', { name: 'Changez' }).first().click();
    await page.waitForLoadState('networkidle');

    // Should be on dashboard
    await expect(page.getByText('Recent Activity')).toBeVisible({ timeout: 5000 });
  });
});
