import { test, expect } from '@playwright/test';
import { login } from './helpers';

test.describe('Projects', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  async function navigateToProjects(page: import('@playwright/test').Page) {
    await page.getByRole('button', { name: 'Projects' }).first().click();
    await page.waitForLoadState('networkidle');
  }

  test('shows project list with changez project', async ({ page }) => {
    await navigateToProjects(page);
    await expect(page.getByPlaceholder('Search projects...')).toBeVisible({ timeout: 5000 });
    await expect(page.getByText('changez').first()).toBeVisible({ timeout: 5000 });
  });

  test('filters projects by name', async ({ page }) => {
    await navigateToProjects(page);
    const searchInput = page.getByPlaceholder('Search projects...');
    await searchInput.fill('changez');
    await expect(page.getByText('changez').first()).toBeVisible({ timeout: 3000 });
  });

  test('shows empty state when search matches nothing', async ({ page }) => {
    await navigateToProjects(page);
    const searchInput = page.getByPlaceholder('Search projects...');
    await searchInput.fill('xxxxxxxxxx');
    await expect(page.getByText(/No projects/i)).toBeVisible({ timeout: 3000 });
  });

  test('navigates to files page when project clicked', async ({ page }) => {
    await navigateToProjects(page);
    // Click the project row button (not the nav "Changez" button)
    const projectBtn = page.locator('button:has(div:has-text("changez"))').first();
    await projectBtn.click();
    await page.waitForLoadState('networkidle');
    await expect(page.getByPlaceholder('Search files...')).toBeVisible({ timeout: 5000 });
  });
});
