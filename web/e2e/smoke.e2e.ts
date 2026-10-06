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
    await expect(page.getByRole('main', { name: 'Alert feed' })).toBeVisible();
    await expect(page.getByText('Waiting for alert events')).toBeAttached();
    await page.goto('/overlay/alerts?demo=1');
    await expect(page.getByText('zinefiend is in the building.')).toBeVisible();
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

  test('rehearses a paused timer and refreshes its audit without changing the schedule', async ({ page }) => {
    let finishRun!: () => void;
    const pendingRun = new Promise<void>((resolve) => { finishRun = resolve; });
    let runs = 0;
    const timer = { id: 'timer-1', name: 'hydrate', enabled: false, intervalSeconds: 300, response: 'Hydrate', action: 'ticker', createdAt: '2026-10-05T12:00:00Z', updatedAt: '2026-10-05T12:00:00Z' };
    await page.route('**/api/bot/**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path.endsWith('/timer-1/run')) {
        expect(route.request().method()).toBe('POST');
        runs += 1;
        if (runs === 1) {
          await pendingRun;
          await route.fulfill({ json: { eventId: 'event-1' } });
        } else {
          await route.fulfill({ status: 503, body: 'bot timer execution unavailable' });
        }
        return;
      }
      const responses: Record<string, unknown> = {
        status: { available: true, commandCount: 0, timerCount: 1, counterCount: 0, quoteCount: 0, auditCount: runs },
        timers: { timers: [timer] },
        commands: { commands: [] },
        counters: { counters: [] },
        quotes: { quotes: [] },
        'api-variables': { variables: [] },
        rules: { rules: [] },
        audit: { events: runs ? [{ id: 'audit-1', createdAt: '2026-10-05T12:01:00Z', source: 'dashboard', result: 'allowed', reason: 'timer_run_once', trigger: 'hydrate' }] : [] },
      };
      await route.fulfill({ json: responses[path.split('/').at(-1)!] ?? {} });
    });
    await page.goto('/dashboard/chat');
    const panel = page.locator('#bot-timers');
    await expect(panel.getByText('Last scheduled run: Never')).toBeVisible();
    await expect(panel.getByText('paused', { exact: true })).toBeVisible();
    await panel.getByRole('button', { name: 'Run once', exact: true }).click();
    await expect(panel.getByRole('button', { name: 'Running…' })).toBeDisabled();
    finishRun();
    await expect(panel.getByText('Sent hydrate. Event event-1. Schedule unchanged.')).toBeVisible();
    await expect(page.getByText('dashboard · timer_run_once')).toBeVisible();
    await expect(panel.getByText('paused', { exact: true })).toBeVisible();
    await expect(panel.getByText('Last scheduled run: Never')).toBeVisible();
    await panel.getByRole('button', { name: 'Run once', exact: true }).click();
    await expect(page.getByText(/bot timer execution unavailable/)).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Run once', exact: true })).toBeEnabled();
    expect(runs).toBe(2);
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
