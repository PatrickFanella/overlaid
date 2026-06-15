import { expect, test } from '@playwright/test';

test.describe('operator browser smoke', () => {
  test('loads critical dashboard and overlay routes', async ({ page }) => {
    await page.goto('/dashboard');
    await expect(page.getByText('over-laid command center')).toBeVisible();

    await page.goto('/dashboard/live');
    await expect(page.getByText('Live ops cockpit')).toBeVisible();
    await expect(page.getByText('Rehearsal audit')).toBeVisible();

    await page.goto('/dashboard/config');
    await expect(page.getByText('OBS import')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Load from OBS' })).toBeVisible();

    await page.goto('/dashboard/control');
    await expect(page.getByRole('heading', { name: 'Stream control', exact: true })).toBeVisible();
    await expect(page.getByText('Quick actions', { exact: true })).toBeVisible();

    await page.goto('/dashboard/bot');
    await expect(page.getByText('Bot operations')).toBeVisible();
    await expect(page.getByText('Bot commands', { exact: true })).toBeVisible();
    await expect(page.getByText('Bot counters and quotes', { exact: true })).toBeVisible();
    await expect(page.getByText('Bot timers', { exact: true })).toBeVisible();
    await expect(page.getByText('Bot audit', { exact: true })).toBeVisible();

    await page.goto('/overlay/alerts');
    await expect(page.getByText('Alert feed')).toBeVisible();
  });
});
