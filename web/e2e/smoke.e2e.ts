import { expect, test } from '@playwright/test';

test.describe('operator browser smoke', () => {
  test('loads critical dashboard and overlay routes', async ({ page }) => {
    await page.goto('/dashboard');
    await expect(page.locator('header').getByText('Broadcast ops console')).toBeVisible();
    const dashboardNav = page.getByRole('navigation', { name: 'Dashboard sections' });
    await expect(dashboardNav.getByText('Monitor', { exact: true })).toBeVisible();
    await expect(dashboardNav.getByText('Operate', { exact: true })).toBeVisible();
    await expect(dashboardNav.getByText('Configure', { exact: true })).toBeVisible();

    await page.goto('/dashboard/live');
    await expect(page.getByText('Live command')).toBeVisible();
    await expect(page.getByText('Rehearsal audit')).toBeVisible();
    await expect(page.getByText('Customize panel layout')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Reset layout' })).toBeVisible();
    await expect(page.getByText('Locked safety panels stay fixed')).toBeVisible();

    await page.goto('/dashboard/control');
    await expect(page.getByRole('heading', { name: 'Stream control', exact: true }).first()).toBeVisible();
    await expect(page.getByText('Quick actions', { exact: true })).toBeVisible();

    await page.goto('/overlay/alerts');
    await expect(page.getByText('Alert feed')).toBeVisible();
  });

  test('shows bot operations safety surfaces', async ({ page }) => {
    await page.goto('/dashboard/bot');
    await expect(page.getByText('Operate with guardrails')).toBeVisible();
    await expect(page.getByText('Bot commands', { exact: true })).toBeVisible();
    await expect(page.getByText('Bot counters and quotes', { exact: true })).toBeVisible();
    await expect(page.getByText('Bot API variables', { exact: true })).toBeVisible();
    await expect(page.getByText('Bot moderation dry-run', { exact: true })).toBeVisible();
    await expect(page.getByText('Nuke dry-run planner', { exact: true })).toBeVisible();
    await expect(page.getByText('I reviewed this dry-run plan')).toBeVisible();
    await expect(page.getByText('Bot timers', { exact: true })).toBeVisible();
    await expect(page.getByText('Bot audit', { exact: true })).toBeVisible();
  });

  test('shows twitch reward queue lifecycle controls', async ({ page }) => {
    await page.goto('/dashboard/config');
    await expect(page.getByText('Configure deliberately')).toBeVisible();
    await expect(page.getByText('Customize panel layout')).toBeVisible();
    await expect(page.getByText('Secret controls stay locked')).toBeVisible();
    await expect(page.getByText('OBS import')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Load from OBS' })).toBeVisible();
    await expect(page.getByText('Reward queue', { exact: true })).toBeVisible();
    await expect(page.getByText('Load manageable rewards, create simple rewards')).toBeVisible();
    await expect(page.getByText('Delete requires confirmation')).toBeVisible();
  });
});
