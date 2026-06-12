import { Page } from '@playwright/test';

export const TOKEN = process.env.E2E_TOKEN || 'HelloChangez';

export async function login(page: Page) {
  await page.goto('/');
  await page.waitForLoadState('networkidle');
  await page.getByText('Authentication Required').waitFor({ timeout: 5000 });
  await page.locator('input[type="password"]').fill(TOKEN);
  await page.getByRole('button', { name: 'Connect' }).click();
  await page.waitForLoadState('networkidle');
}
