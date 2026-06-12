import { test, expect } from '@playwright/test';
import { login } from './helpers';

test.describe('Dashboard', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('shows stat cards after loading', async ({ page }) => {
    await expect(page.getByRole('button', { name: /Projects$/ }).first()).toBeVisible({ timeout: 5000 });
    await expect(page.getByRole('button', { name: /Files$/ }).first()).toBeVisible({ timeout: 3000 });
    await expect(page.getByRole('button', { name: /Versions$/ }).first()).toBeVisible({ timeout: 3000 });
  });

  test('shows change sources section', async ({ page }) => {
    await expect(page.getByText('Change Sources')).toBeVisible({ timeout: 5000 });
  });

  test('shows recent activity section', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Recent Activity' })).toBeVisible({ timeout: 5000 });
  });

  test('has navigation with app name', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Changez' }).first()).toBeVisible({ timeout: 5000 });
  });

  test('can navigate to projects page', async ({ page }) => {
    await page.locator('button').filter({ hasText: 'Projects' }).first().click();
    await page.waitForLoadState('networkidle');
    await expect(page.getByPlaceholder('Search projects...')).toBeVisible({ timeout: 5000 });
  });
});
