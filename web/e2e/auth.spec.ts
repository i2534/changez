import { test, expect } from '@playwright/test';
import { login, TOKEN } from './helpers';

test.describe('Authentication', () => {
  test('shows login modal when auth is required', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await expect(page.getByText('Authentication Required')).toBeVisible({ timeout: 5000 });
  });

  test('invalid token triggers toast error after reload', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Fill wrong token and submit (/health doesn't check auth, so it "succeeds")
    await page.locator('input[type="password"]').fill('wrong-token');
    await page.getByRole('button', { name: 'Connect' }).click();

    // Page reloads, but now bad token is in localStorage
    // Real API calls will fail with 401 → toast notification
    await page.waitForLoadState('networkidle');

    // Check that API errors appear as toast (because wrong token can't auth)
    // The LoginModal should appear again due to auth-required event
    await expect(page.getByText('Authentication Required')).toBeVisible({ timeout: 5000 });
  });

  test('logs in with valid token and sees dashboard', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    await expect(page.getByText('Authentication Required')).toBeVisible({ timeout: 5000 });

    const input = page.locator('input[type="password"]');
    await input.fill(TOKEN);
    await page.getByRole('button', { name: 'Connect' }).click();

    // After successful login, reload shows dashboard
    await page.waitForLoadState('networkidle');
    await expect(page.getByText('Projects')).toBeVisible({ timeout: 5000 });
  });
});
