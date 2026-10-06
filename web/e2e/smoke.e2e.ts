import { expect, test } from '@playwright/test';

test.describe('operator browser smoke', () => {
  test('loads the desk shell and critical routes', async ({ page }) => {
    await page.goto('/dashboard');
    const nav = page.getByRole('navigation', { name: 'Dashboard sections' });
    await expect(nav.getByText('Show', { exact: true })).toBeVisible();
    await expect(nav.getByText('Prepare', { exact: true })).toBeVisible();
    await expect(nav.getByText('Review', { exact: true })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'On air', exact: true })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Run configured actions' })).toBeVisible();
    await expect(page.getByText('Off air', { exact: true })).toBeVisible();

    await page.goto('/dashboard/health');
    await expect(page.getByRole('heading', { name: 'Backend readiness' })).toBeVisible();
    await page.getByText('Customize panel layout').click();
    await expect(page.getByRole('button', { name: 'Reset layout' })).toBeVisible();
    await expect(page.getByText('Locked safety panels stay fixed outside this list.', { exact: true })).toBeVisible();

    await page.goto('/dashboard/scenes');
    await expect(page.getByRole('heading', { name: 'OBS controls' })).toBeVisible();

    await page.goto('/overlay/alerts');
    await expect(page.getByText('Waiting for alert events')).toBeVisible();
  });

  test('keeps legacy console paths working', async ({ page }) => {
    await page.goto('/dashboard/control');
    await expect(page).toHaveURL(/\/dashboard\/scenes$/);
    await page.goto('/dashboard/config');
    await expect(page).toHaveURL(/\/dashboard\/settings$/);
  });

  test('shows bot operations safety surfaces', async ({ page }) => {
    await page.goto('/dashboard/chat');
    await expect(page.getByText('guarded destructive actions')).toBeVisible();
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
    await page.goto('/dashboard/chat');
    await expect(page.getByRole('heading', { name: 'Reward queue' })).toBeVisible();
    await expect(page.getByText('Load manageable rewards, create simple rewards')).toBeVisible();
    await expect(page.getByText('Delete requires confirmation')).toBeVisible();

    await page.goto('/dashboard/settings');
    await expect(page.getByText('Configure deliberately')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Load from OBS' })).toBeVisible();
  });

  test('previews every overlay in demo mode', async ({ page }) => {
    await page.goto('/overlay/brb?demo=1');
    await expect(page.getByText('Swapping tapes. Chat stays open.')).toBeVisible();
    await page.goto('/overlay/starting?minutes=10');
    await expect(page.getByText('10:00')).toBeVisible();
  });
});
