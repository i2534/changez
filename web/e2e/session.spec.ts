import { test, expect } from '@playwright/test';
import { login } from './helpers';

test.describe('Session Analysis', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('shows session page for valid session ID pattern', async ({ page }) => {
    await page.goto('/projects/changez/session/invalid-session-id');
    await page.waitForLoadState('networkidle');

    // Session page renders even for unknown IDs (shows empty state)
    await expect(page.getByRole('heading', { name: 'Session Analysis' })).toBeVisible({ timeout: 5000 });
    await expect(page.getByText('invalid-session-id').first()).toBeVisible({ timeout: 3000 });
  });

  test('shows empty changes state for unknown session', async ({ page }) => {
    await page.goto('/projects/changez/session/nonexistent');
    await page.waitForLoadState('networkidle');

    // Shows the session page with 0 changes
    await expect(page.getByRole('heading', { name: 'Session Analysis' })).toBeVisible({ timeout: 5000 });
    await expect(page.getByText('No changes in this session.')).toBeVisible({ timeout: 3000 });
  });
});
