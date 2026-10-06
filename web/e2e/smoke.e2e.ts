import { expect, test } from '@playwright/test';
import { readFile } from 'node:fs/promises';

test.describe('operator browser smoke', () => {
  test('holds session controls until the ended report returns and preserves a failed report receipt', async ({ page }) => {
    await page.addInitScript(() => sessionStorage.setItem('overlaid.operatorSecret.session.v1', 'operator-fixture'));
    const current = { id: 'pending-report', title: 'Fixture session', kind: 'rehearsal', startedAt: '2026-10-06T00:00:00Z', eventCount: 0 };
    const ended = { ...current, endedAt: '2026-10-06T01:00:00Z' };
    await page.route('**/api/stream/session', (route) => route.fulfill({ json: { current, recent: [current] } }));
    await page.route('**/api/stream/session/end', (route) => route.fulfill({ json: ended }));
    let reports = 0;
    let releaseReport!: () => void;
    const blocked = new Promise<void>((resolve) => { releaseReport = resolve; });
    await page.route('**/api/stream/session/pending-report?*', async (route) => {
      reports++;
      if (reports === 1) {
        await blocked;
        await route.fulfill({ status: 503, body: 'fixture report offline' });
      } else await route.fulfill({ json: { session: ended, events: [], snapshots: [] } });
    });
    await page.goto('/dashboard/sessions');
    await page.getByRole('button', { name: 'End session', exact: true }).click();
    await expect.poll(() => reports).toBe(1);
    await expect(page.getByRole('button', { name: 'Start rehearsal', exact: true })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Open latest report', exact: true })).toBeDisabled();
    releaseReport();
    await expect(page.getByText('Fixture session ended. Report unavailable; retry from Recent sessions.', { exact: true })).toBeVisible();
    await expect(page.getByText(/fixture report offline/)).toBeVisible();
    await expect(page.getByRole('button', { name: 'Start rehearsal', exact: true })).toBeEnabled();
    await page.getByRole('button', { name: 'Open latest report', exact: true }).click();
    await expect(page.getByText('Fixture session report loaded.', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Export JSON', exact: true })).toBeVisible();
  });

  test('reports unavailable preflight evidence accurately and clears a failed rehearsal after cleanup', async ({ page }) => {
    await page.addInitScript(() => sessionStorage.setItem('overlaid.operatorSecret.session.v1', 'operator-fixture'));
    const current = { id: 'preflight-fixture', title: 'Preflight rehearsal', kind: 'rehearsal', startedAt: '2026-10-06T00:00:00Z', eventCount: 0 };
    const ended = { ...current, endedAt: '2026-10-06T01:00:00Z' };
    let starts = 0, ends = 0;
    await page.route('**/api/stream/session', (route) => route.fulfill({ json: { recent: [] } }));
    await page.route('**/api/stream/session/start', (route) => { starts++; return route.fulfill({ json: current }); });
    await page.route('**/api/stream/session/end', (route) => { ends++; return route.fulfill({ json: ended }); });
    await page.route('**/api/stream/session/preflight-fixture?*', (route) => starts === 1
      ? route.fulfill({ status: 503, body: 'fixture evidence offline' })
      : route.fulfill({ json: { session: ended, events: [], snapshots: [] } }));
    await page.route('**/api/events/test-alert', (route) => starts === 1
      ? route.fulfill({ status: 204 }) : route.fulfill({ status: 503, body: 'fixture alert failed' }));
    await page.route('**/api/events/brb', (route) => route.fulfill({ status: 204 }));
    await page.goto('/dashboard/sessions');
    await page.getByRole('button', { name: 'Run preflight rehearsal', exact: true }).click();
    await expect(page.getByText('Preflight rehearsal complete. Report unavailable; retry from Recent sessions.', { exact: true })).toBeVisible();
    await expect(page.getByText(/fixture evidence offline/)).toBeVisible();
    expect(ends).toBe(1);
    await page.getByRole('button', { name: 'Run preflight rehearsal', exact: true }).click();
    await expect(page.getByText(/Preflight failed:.*fixture alert failed/)).toBeVisible();
    expect(ends).toBe(2);
    await expect(page.getByRole('button', { name: 'End session', exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Start rehearsal', exact: true })).toBeEnabled();
    await expect(page.getByRole('list', { name: 'Preflight steps' })).toContainText('Sample alert: fail');
  });

  test('confirms an operator sequence, blocks pending controls, logs failure, and allows retry', async ({ page }) => {
    await page.addInitScript(() => {
      sessionStorage.setItem('overlaid.operatorSecret.session.v1', 'operator-fixture');
      localStorage.setItem('overlaid.dashboard.config.v1', JSON.stringify({ quickActions: [
        { id: 'scene-fixture', label: 'Scene fixture', kind: 'setScene', sceneName: 'Fixture' },
        { id: 'refresh-fixture', label: 'Refresh fixture', kind: 'refreshSource', inputName: 'Fixture' },
        { id: 'clear-fixture', label: 'Clear fixture', kind: 'clearOverlays', confirm: true },
        { id: 'sequence-fixture', label: 'Sequence fixture', kind: 'sequence', actionIds: ['scene-fixture', 'refresh-fixture', 'clear-fixture'], stopOnError: true, confirm: true },
      ] }));
    });
    const calls: string[] = [];
    let releaseScene!: () => void;
    const blocked = new Promise<void>((resolve) => { releaseScene = resolve; });
    await page.route('**/api/obs/scene', async (route) => {
      calls.push('scene');
      expect(route.request().headers()['x-overlay-operator-secret']).toBe('operator-fixture');
      await blocked;
      await route.fulfill({ status: 204 });
    });
    await page.route('**/api/obs/source/refresh', async (route) => {
      calls.push('refresh');
      await route.fulfill({ status: 503, body: 'fixture source unavailable' });
    });
    await page.route('**/api/events/clear', async (route) => {
      calls.push('clear');
      await route.fulfill({ status: 204 });
    });
    await page.goto('/dashboard/scenes');
    await page.getByRole('button', { name: 'Sequence fixture', exact: true }).click();
    expect(calls).toEqual([]);
    await page.getByRole('button', { name: 'Confirm Sequence fixture', exact: true }).click();
    await expect.poll(() => calls).toEqual(['scene']);
    await expect(page.getByRole('button', { name: 'Sequence fixture', exact: true })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Clear fixture', exact: true })).toBeDisabled();
    await expect(page.getByRole('log')).toContainText('Action started.');
    releaseScene();
    await expect(page.getByRole('log')).toContainText('fixture source unavailable');
    expect(calls).toEqual(['scene', 'refresh']);
    await expect(page.getByRole('button', { name: 'Sequence fixture', exact: true })).toBeEnabled();
    await page.getByRole('button', { name: 'Clear fixture', exact: true }).click();
    expect(calls).toEqual(['scene', 'refresh']);
    await page.getByRole('button', { name: 'Confirm Clear fixture', exact: true }).click();
    await expect.poll(() => calls).toEqual(['scene', 'refresh', 'clear']);
    await expect(page.getByRole('log')).toContainText('Action completed.');
    await expect(page.getByRole('button', { name: 'Clear fixture', exact: true })).toBeEnabled();
  });

  test('preserves omitted layout preferences when reordering and resetting Health panels', async ({ page }) => {
    await page.addInitScript(() => {
      const key = 'overlaid.dashboard.config.v1';
      if (!localStorage.getItem(key)) localStorage.setItem(key, JSON.stringify({ panelLayouts: { live: [
        { id: 'event-history', visible: false, order: 1 },
        { id: 'readiness', visible: true, order: 2 },
        { id: 'session-log', visible: false, order: 3 },
        { id: 'preflight', visible: true, order: 4 },
        { id: 'eventsub', visible: true, order: 5 },
        { id: 'build-info', visible: true, order: 6 },
        { id: 'checklist', visible: true, order: 7 },
      ] } }));
    });
    const stored = () => page.evaluate(() => JSON.parse(localStorage.getItem('overlaid.dashboard.config.v1')!).panelLayouts.live as { id: string; order: number; visible: boolean }[]);
    await page.goto('/dashboard/health');
    await page.getByText('Customize panel layout', { exact: true }).click();
    await page.getByRole('button', { name: 'Move Live preflight up', exact: true }).click();
    const moved = await stored();
    expect(moved.find((item) => item.id === 'preflight')?.order).toBe(2);
    expect(moved.find((item) => item.id === 'readiness')?.order).toBe(4);
    expect(moved.find((item) => item.id === 'event-history')).toMatchObject({ visible: false, order: 1 });
    expect(moved.find((item) => item.id === 'session-log')).toMatchObject({ visible: false, order: 3 });
    await page.getByRole('button', { name: 'Hide Readiness', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Backend readiness', exact: true })).toHaveCount(0);
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Backend readiness', exact: true })).toHaveCount(0);
    await page.getByText('Customize panel layout', { exact: true }).click();
    await page.getByRole('button', { name: 'Reset layout', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Backend readiness', exact: true })).toBeVisible();
    const reset = await stored();
    expect(reset.find((item) => item.id === 'readiness')).toMatchObject({ visible: true, order: 2 });
    expect(reset.find((item) => item.id === 'event-history')).toMatchObject({ visible: false, order: 1 });
    expect(reset.find((item) => item.id === 'session-log')).toMatchObject({ visible: false, order: 3 });
    expect(new Set(reset.map((item) => item.order)).size).toBe(reset.length);
    await expect(page.getByRole('button', { name: /Hide.*[Rr]ecovery/ })).toHaveCount(0);
  });

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
    let configurationChanged = false;
    let auditUnavailable = false;
    const timer = { id: 'timer-1', name: 'hydrate', enabled: false, intervalSeconds: 300, response: 'Hydrate', action: 'ticker', createdAt: '2026-10-05T12:00:00Z', updatedAt: '2026-10-05T12:00:00Z' };
    await page.route('**/api/bot/**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path.endsWith('/audit') && auditUnavailable) {
        await route.fulfill({ status: 503, body: 'audit temporarily unavailable' });
        return;
      }
      if (path.endsWith('/timers/timer-1') && route.request().method() === 'PUT') {
        timer.enabled = route.request().postDataJSON().enabled;
        configurationChanged = true;
        await route.fulfill({ json: timer });
        return;
      }
      if (path.endsWith('/timer-1/run')) {
        expect(route.request().method()).toBe('POST');
        runs += 1;
        if (runs === 1) {
          await pendingRun;
          await route.fulfill({ json: { eventId: 'event-1' } });
        } else if (runs === 2) {
          await route.fulfill({ status: 503, body: 'bot timer execution unavailable' });
        } else {
          auditUnavailable = true;
          await route.fulfill({ json: { eventId: 'event-3' } });
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
        audit: { events: runs ? [{ id: 'audit-1', createdAt: '2026-10-05T12:01:00Z', source: 'dashboard', result: 'allowed', reason: configurationChanged ? 'timer_configuration_changed' : 'timer_run_once', trigger: 'hydrate' }] : [] },
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
    await panel.getByRole('button', { name: 'Enable', exact: true }).click();
    await expect(panel.getByText('enabled', { exact: true })).toBeVisible();
    await expect(page.getByText('dashboard · timer_configuration_changed')).toBeVisible();
    await panel.getByRole('button', { name: 'Run once', exact: true }).click();
    await expect(panel.getByText('Sent hydrate. Event event-3. Schedule unchanged.')).toBeVisible();
    await expect(panel.getByRole('alert')).toContainText('Timer operation succeeded. Status/audit refresh failed:');
    await expect(panel.getByRole('button', { name: 'Run once', exact: true })).toBeEnabled();
    expect(runs).toBe(3);
  });

  test('edits saved timers, preserves failed drafts, and keeps newer audit after a late load', async ({ page }) => {
    let releaseInitialAudit!: () => void;
    const initialAuditReady = new Promise<void>((resolve) => { releaseInitialAudit = resolve; });
    let writes = 0;
    let saved = false;
    let deleted = false;
    let timer = { id: 'timer-1', name: 'hydrate_edit', enabled: false, intervalSeconds: 300, response: 'Original response', action: 'ticker', createdAt: '2026-10-05T12:00:00Z', updatedAt: '2026-10-05T12:00:00Z', lastRunAt: '2026-10-05T12:00:00Z' };
    await page.route('**/api/bot/**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path.endsWith('/timers') && route.request().method() === 'POST') {
        const payload = route.request().postDataJSON();
        expect(payload.id).toBe('timer-1');
        expect(payload.enabled).toBe(false);
        writes += 1;
        if (writes === 1) {
          await route.fulfill({ status: 503, body: 'save temporarily unavailable' });
          return;
        }
        timer = { ...timer, ...payload };
        saved = true;
        await route.fulfill({ json: timer });
        return;
      }
      if (path.endsWith('/timers/timer-1') && route.request().method() === 'DELETE') {
        deleted = true;
        await route.fulfill({ status: 204 });
        return;
      }
      if (path.endsWith('/audit') && !saved) {
        await initialAuditReady;
        await route.fulfill({ json: { events: [] } });
        return;
      }
      const responses: Record<string, unknown> = {
        status: { available: true, commandCount: 0, timerCount: deleted ? 0 : 1, counterCount: 0, quoteCount: 0, auditCount: saved ? 1 : 0 },
        timers: { timers: deleted ? [] : [timer] },
        commands: { commands: [] }, counters: { counters: [] }, quotes: { quotes: [] }, rules: { rules: [] },
        audit: { events: saved ? [{ id: 'audit-edit', createdAt: '2026-10-05T12:01:00Z', source: 'dashboard', result: 'allowed', reason: 'timer_configuration_changed', trigger: 'hydrate_edit' }] : [] },
      };
      await route.fulfill({ json: responses[path.split('/').at(-1)!] ?? {} });
    });
    await page.goto('/dashboard/chat');
    const panel = page.locator('#bot-timers');
    await panel.getByRole('button', { name: 'Edit', exact: true }).click();
    await expect(panel.getByLabel('Timer name')).toHaveValue('hydrate_edit');
    await panel.getByLabel('Response').fill('Unsaved response');
    await panel.getByRole('button', { name: 'Cancel edit' }).click();
    await expect(panel.getByLabel('Timer name')).toHaveValue('');
    expect(writes).toBe(0);

    await panel.getByRole('button', { name: 'Edit', exact: true }).click();
    await panel.getByLabel('Interval seconds').fill('90000');
    await expect(panel.getByRole('button', { name: 'Save changes' })).toBeDisabled();
    await panel.getByLabel('Interval seconds').fill('30.5');
    await expect(panel.getByRole('button', { name: 'Save changes' })).toBeDisabled();
    await panel.getByLabel('Interval seconds').fill('600');
    await panel.getByLabel('Response').fill('Updated response');
    await panel.getByLabel('Action').selectOption('alert');
    await panel.getByRole('button', { name: 'Save changes' }).click();
    await expect(panel.getByRole('alert')).toContainText('save temporarily unavailable');
    await expect(panel.getByLabel('Response')).toHaveValue('Updated response');
    await expect(panel.getByRole('button', { name: 'Save changes' })).toBeEnabled();
    await panel.getByRole('button', { name: 'Save changes' }).click();
    await expect(panel.getByText('Updated hydrate_edit.', { exact: true })).toBeVisible();
    await expect(panel.getByText('every 600s · alert')).toBeVisible();
    await expect(panel.getByText('Updated response', { exact: true })).toBeVisible();
    await expect(panel.getByText('paused', { exact: true })).toBeVisible();
    await expect(panel.getByLabel('Timer name')).toHaveValue('');
    await expect(page.getByText('dashboard · timer_configuration_changed')).toBeVisible();
    releaseInitialAudit();
    await expect(page.locator('#bot-api-variables').getByText('No API variables configured yet.')).toBeVisible();
    await expect(page.getByText('dashboard · timer_configuration_changed')).toBeVisible();
    expect(writes).toBe(2);
    expect(timer.lastRunAt).toBe('2026-10-05T12:00:00Z');

    await panel.getByRole('button', { name: 'Edit', exact: true }).click();
    await panel.getByRole('button', { name: 'Delete', exact: true }).click();
    await expect(panel.getByText('No bot timers configured yet.')).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Cancel edit' })).toHaveCount(0);
    await expect(panel.getByLabel('Timer name')).toHaveValue('');
  });

  test('keeps available bot panels when API variables cannot load', async ({ page }) => {
    await page.route('**/api/bot/**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path.endsWith('/api-variables')) {
        await route.fulfill({ status: 503, body: 'variables offline' });
        return;
      }
      const responses: Record<string, unknown> = {
        status: { available: true, commandCount: 0, timerCount: 1, counterCount: 0, quoteCount: 0, auditCount: 0 },
        timers: { timers: [{ id: 'timer-1', name: 'hydrate_available', enabled: false, intervalSeconds: 300, response: 'Hydrate', action: 'ticker' }] },
        commands: { commands: [] }, counters: { counters: [] }, quotes: { quotes: [] }, rules: { rules: [] }, audit: { events: [] },
      };
      await route.fulfill({ json: responses[path.split('/').at(-1)!] ?? {} });
    });
    await page.goto('/dashboard/chat');
    await expect(page.locator('#bot-api-variables').getByText(/variables offline/)).toBeVisible();
    await expect(page.locator('#bot-timers').getByText('hydrate_available', { exact: true })).toBeVisible();
    await expect(page.locator('#bot-timers').getByRole('button', { name: 'Run once' })).toBeEnabled();
    await expect(page.getByText('storage available', { exact: true })).toBeVisible();
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

  test('saves reward ID rules and matches renamed reward queue notes', async ({ page }) => {
    let saved = false;
    const rules = { chatAliases: [], channelPointRules: [
      { rewardId: 'stable-id', rewardTitle: 'Original title', action: 'alert', enabled: true, fulfillmentNote: 'Stable reward note' },
      { rewardTitle: 'Renamed reward', action: 'ignore', enabled: true, fulfillmentNote: 'Title fallback note' },
    ] };
    await page.route('**/api/twitch/**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path.endsWith('/interactions')) {
        if (route.request().method() === 'PUT') {
          const payload = route.request().postDataJSON().rules;
          expect(payload.channelPointRules[0].rewardId).toBe('stable-id');
          expect(payload.channelPointRules[0].rewardTitle).toBe('Edited label');
          saved = true;
          rules.channelPointRules = payload.channelPointRules;
        }
        await route.fulfill({ json: { rules } });
      } else if (path.endsWith('/redemptions')) {
        await route.fulfill({ json: { redemptions: [{ id: 'redemption-1', user_name: 'Viewer', user_input: 'Hello', status: 'UNFULFILLED', redeemed_at: '2026-10-06T00:00:00Z', reward: { id: 'stable-id', title: 'Renamed reward' } }] } });
      } else if (path.endsWith('/rewards')) {
        await route.fulfill({ json: { rewards: [{ id: 'stable-id', title: 'Renamed reward', cost: 100, is_enabled: true }] } });
      } else {
        await route.fallback();
      }
    });
    await page.goto('/dashboard/settings');
    await page.getByLabel('Secret header').fill('browser-fixture');
    await page.goto('/dashboard/chat');
    const editor = page.locator('#twitch-rules');
    await expect(editor.getByLabel('Reward ID (optional)').first()).toHaveValue('stable-id');
    await editor.getByLabel('Reward title (label)', { exact: true }).fill('Edited label');
    await editor.getByRole('button', { name: 'Save rules', exact: true }).click();
    await expect.poll(() => saved).toBe(true);
    const queue = page.locator('#reward-queue');
    await queue.getByRole('button', { name: 'Load rewards', exact: true }).click();
    await expect(queue.getByText('Reward ID: stable-id', { exact: true })).toBeVisible();
    await expect(queue.getByText('Fulfillment note: Stable reward note')).toBeVisible();
    await expect(queue.getByText('Fulfillment note: Title fallback note')).toHaveCount(0);
    await queue.getByRole('button', { name: 'Load unfulfilled', exact: true }).click();
    await expect(queue.getByText('Fulfillment note: Stable reward note')).toHaveCount(2);
  });

  test('counts Unicode reward text consistently before create and edit writes', async ({ page }) => {
    const title = '🌊'.repeat(45);
    const prompt = '水🌊'.repeat(100);
    let writes = 0;
    let reward = { id: 'unicode-reward', title, cost: 100, prompt, is_enabled: false };
    await page.route('**/api/twitch/rewards**', async (route) => {
      const method = route.request().method();
      if (method === 'POST') {
        writes++;
        expect(route.request().postDataJSON()).toMatchObject({ title, prompt });
        await route.fulfill({ json: { reward } });
      } else if (method === 'PATCH') {
        writes++;
        expect(route.request().postDataJSON()).toEqual({ prompt: '🌊'.repeat(200) });
        reward = { ...reward, prompt: '🌊'.repeat(200) };
        await route.fulfill({ json: { reward } });
      } else await route.fulfill({ json: { rewards: [reward] } });
    });
    await page.goto('/dashboard/chat');
    const queue = page.locator('#reward-queue');
    await queue.getByLabel('New reward title').fill(title + '🌊');
    await queue.getByRole('button', { name: 'Create reward', exact: true }).click();
    await expect(queue.getByText('Reward title must be 1-45 characters.', { exact: true })).toBeVisible();
    expect(writes).toBe(0);
    await queue.getByLabel('New reward title').fill(title);
    await queue.getByLabel('Prompt', { exact: true }).first().fill(prompt + '水');
    await queue.getByRole('button', { name: 'Create reward', exact: true }).click();
    await expect(queue.getByText('Reward prompt must be 200 characters or fewer.', { exact: true })).toBeVisible();
    expect(writes).toBe(0);
    await queue.getByLabel('Prompt', { exact: true }).first().fill(prompt);
    await queue.getByRole('button', { name: 'Create reward', exact: true }).click();
    await expect(queue.getByText('Reward created.', { exact: true })).toBeVisible();
    expect(writes).toBe(1);
    await queue.getByLabel('Prompt', { exact: true }).last().fill('🌊'.repeat(201));
    await queue.getByRole('button', { name: 'Save reward fields', exact: true }).click();
    await expect(queue.getByText('Reward prompt must be 200 characters or fewer.', { exact: true })).toBeVisible();
    expect(writes).toBe(1);
    await queue.getByLabel('Prompt', { exact: true }).last().fill('🌊'.repeat(200));
    await queue.getByRole('button', { name: 'Save reward fields', exact: true }).click();
    await expect(queue.getByText('Reward fields saved.', { exact: true })).toBeVisible();
    expect(writes).toBe(2);
  });

  test('guards reward writes and preserves successful receipts when reload fails', async ({ page }) => {
    let releaseOldLoad!: () => void;
    const oldLoading = new Promise<void>((resolve) => { releaseOldLoad = resolve; });
    let reads = 0;
    let oldLoadFinished = false;
    let releaseCreate!: () => void;
    const creating = new Promise<void>((resolve) => { releaseCreate = resolve; });
    let writes = 0;
    let reloadFails = false;
    let editFails = true;
    let reward = { id: 'created-reward', title: 'Hydrate', cost: 100, prompt: '', is_enabled: true, is_paused: false, is_user_input_required: false };
    await page.route('**/api/twitch/rewards**', async (route) => {
      const method = route.request().method();
      if (method === 'GET') {
        reads++;
        if (reads === 1) {
          await oldLoading;
          await route.fulfill({ json: { rewards: [{ ...reward, id: 'older-reward', title: 'Older reward' }] } });
          oldLoadFinished = true;
          return;
        }
        await route.fulfill(reloadFails ? { status: 503, body: 'reward reload unavailable' } : { json: { rewards: [reward] } });
      } else if (method === 'POST') {
        writes++;
        await creating;
        reloadFails = true;
        await route.fulfill({ json: { reward, result: 'created' } });
      } else if (method === 'PATCH') {
        writes++;
        const patch = route.request().postDataJSON();
        if ('prompt' in patch) {
          expect(patch).toEqual({ prompt: 'Remember water' });
          if (editFails) { await route.fulfill({ status: 503, body: 'reward edit unavailable' }); return; }
          reward = { ...reward, prompt: patch.prompt };
        } else { reward = { ...reward, is_paused: patch.paused }; }
        await route.fulfill({ json: { reward, result: 'updated' } });
      } else if (method === 'DELETE') {
        writes++;
        await route.fulfill({ json: { result: 'deleted' } });
      } else { await route.fallback(); }
    });
    await page.goto('/dashboard/chat');
    const queue = page.locator('#reward-queue');
    await queue.getByRole('button', { name: 'Load rewards', exact: true }).click();
    await expect.poll(() => reads).toBe(1);
    await queue.getByLabel('New reward title').fill('Hydrate');
    await queue.getByLabel('Global cooldown seconds').first().fill('604801');
    await queue.getByRole('button', { name: 'Create reward', exact: true }).click();
    await expect(queue.getByText('Global cooldown seconds must be 604800 or fewer.', { exact: true })).toBeVisible();
    expect(writes).toBe(0);
    await queue.getByLabel('Global cooldown seconds').first().fill('0');
    await queue.getByRole('button', { name: 'Create reward', exact: true }).click();
    await expect.poll(() => writes).toBe(1);
    await expect(queue.getByRole('button', { name: 'Create reward', exact: true })).toBeDisabled();
    await expect(queue.getByRole('button', { name: 'Load rewards', exact: true })).toBeDisabled();
    await expect(queue.getByLabel('New reward title')).toBeDisabled();
    releaseCreate();
    await expect(queue.getByText('Reward created.', { exact: true })).toBeVisible();
    await expect(queue.getByText(/reward reload unavailable/)).toBeVisible();
    await expect(queue.getByText('Reward ID: created-reward', { exact: true })).toBeVisible();
    releaseOldLoad();
    await expect.poll(() => oldLoadFinished).toBe(true);
    await expect(queue.getByText('Reward ID: created-reward', { exact: true })).toBeVisible();
    await expect(queue.getByText('Reward ID: older-reward', { exact: true })).toHaveCount(0);
    await queue.getByLabel('Global cooldown seconds').last().fill('604801');
    await queue.getByRole('button', { name: 'Save reward fields', exact: true }).click();
    await expect(queue.getByText('Global cooldown seconds must be 604800 or fewer.', { exact: true })).toBeVisible();
    expect(writes).toBe(1);
    await queue.getByLabel('Global cooldown seconds').last().fill('0');
    await queue.getByLabel('Prompt', { exact: true }).last().fill('Remember water');
    await queue.getByRole('button', { name: 'Save reward fields', exact: true }).click();
    await expect(queue.getByText(/reward edit unavailable/)).toBeVisible();
    await expect(queue.getByLabel('Prompt', { exact: true }).last()).toHaveValue('Remember water');
    editFails = false;
    await queue.getByRole('button', { name: 'Save reward fields', exact: true }).click();
    await expect(queue.getByText('Reward fields saved.', { exact: true })).toBeVisible();
    await expect(queue.getByLabel('Prompt', { exact: true }).last()).toHaveValue('Remember water');
    await queue.getByRole('button', { name: 'Pause reward', exact: true }).click();
    await expect(queue.getByText('Reward lifecycle updated.', { exact: true })).toBeVisible();
    await expect(queue.getByRole('button', { name: 'Resume reward', exact: true })).toBeVisible();
    await queue.getByLabel('Confirm delete', { exact: true }).check();
    await queue.getByRole('button', { name: 'Delete selected', exact: true }).click();
    await expect(queue.getByText('Reward deleted.', { exact: true })).toBeVisible();
    await expect(queue.getByText('Reward ID: created-reward', { exact: true })).toHaveCount(0);
    expect(writes).toBe(5);
  });

  test('guards redemption writes and retains fulfillment receipts after reload failure', async ({ page }) => {
    let releaseFulfill!: () => void;
    const fulfilling = new Promise<void>((resolve) => { releaseFulfill = resolve; });
    let writes = 0;
    let reloadFails = false;
    const reward = { id: 'reward-1', title: 'Hydrate', cost: 100, is_enabled: true };
    await page.route('**/api/twitch/rewards**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (route.request().method() === 'POST') {
        writes++;
        if (path.endsWith('/redemption-1')) { expect(route.request().postDataJSON().status).toBe('FULFILLED'); await fulfilling; }
        else { expect(route.request().postDataJSON().status).toBe('CANCELED'); }
        reloadFails = true;
        await route.fulfill({ json: { result: 'accepted' } });
      } else if (path.endsWith('/redemptions')) {
        await route.fulfill(reloadFails ? { status: 503, body: 'redemption reload unavailable' } : { json: { redemptions: [1, 2].map((id) => ({ id: `redemption-${id}`, user_name: `Viewer ${id}`, status: 'UNFULFILLED', redeemed_at: '2026-10-06T00:00:00Z', reward })) } });
      } else { await route.fulfill({ json: { rewards: [reward] } }); }
    });
    await page.goto('/dashboard/chat');
    const queue = page.locator('#reward-queue');
    await queue.getByRole('button', { name: 'Load rewards', exact: true }).click();
    await queue.getByRole('button', { name: 'Load unfulfilled', exact: true }).click();
    await queue.getByRole('button', { name: 'Fulfill', exact: true }).first().click();
    await expect.poll(() => writes).toBe(1);
    await expect(queue.getByRole('button', { name: 'Fulfill', exact: true }).last()).toBeDisabled();
    await expect(queue.getByRole('button', { name: 'Cancel', exact: true }).last()).toBeDisabled();
    releaseFulfill();
    await expect(queue.getByText('Redemption fulfilled.', { exact: true })).toBeVisible();
    await expect(queue.getByText(/redemption reload unavailable/)).toBeVisible();
    await expect(queue.getByText('Viewer 1 · Hydrate', { exact: true })).toHaveCount(0);
    await expect(queue.getByText('Viewer 2 · Hydrate', { exact: true })).toBeVisible();
    await queue.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(queue.getByText('Redemption canceled.', { exact: true })).toBeVisible();
    await expect(queue.getByText('No unfulfilled redemptions loaded.', { exact: true })).toBeVisible();
    expect(writes).toBe(2);
  });

  test('edits commands independently, retries failed drafts, and preserves successful writes after refresh failure', async ({ page }) => {
    let releaseSave!: () => void;
    const delayedSave = new Promise<void>((resolve) => { releaseSave = resolve; });
    let writes = 0;
    let deleted = false;
    let auditUnavailable = false;
    let command = { id: 'command-edit', name: 'welcome', enabled: false, response: 'Original response', action: 'alert', responseMode: 'both', roleRequired: 'moderator', cooldownSeconds: 45 };
    await page.route('**/api/bot/**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      const method = route.request().method();
      if (path.endsWith('/api-variables') || (path.endsWith('/audit') && auditUnavailable)) {
        await route.fulfill({ status: 503, body: 'summary temporarily unavailable' });
        return;
      }
      if (path.endsWith('/commands') && method === 'POST') {
        const payload = route.request().postDataJSON();
        expect(payload.id).toBe('command-edit');
        expect(payload.responseMode).toBe('both');
        expect(payload.roleRequired).toBe('moderator');
        expect(payload.enabled).toBe(false);
        expect(payload.action).toBe('alert');
        writes += 1;
        if (writes === 1) await route.fulfill({ status: 503, body: 'command save unavailable' });
        else {
          await delayedSave;
          command = { ...command, ...payload };
          auditUnavailable = true;
          await route.fulfill({ json: command });
        }
        return;
      }
      if (path.endsWith('/commands/command-edit') && method === 'PUT') {
        command = { ...command, enabled: route.request().postDataJSON().enabled };
        await route.fulfill({ json: command });
        return;
      }
      if (path.endsWith('/commands/command-edit') && method === 'DELETE') {
        deleted = true;
        await route.fulfill({ status: 204 });
        return;
      }
      const responses: Record<string, unknown> = {
        status: { available: true, commandCount: deleted ? 0 : 1, timerCount: 0, counterCount: 0, quoteCount: 0, auditCount: 0 },
        commands: { commands: deleted ? [] : [command] }, timers: { timers: [] }, counters: { counters: [] }, quotes: { quotes: [] }, rules: { rules: [] }, audit: { events: [] },
      };
      await route.fulfill({ json: responses[path.split('/').at(-1)!] ?? {} });
    });
    await page.goto('/dashboard/chat');
    const panel = page.locator('#bot-commands');
    await panel.getByRole('button', { name: 'Edit', exact: true }).click();
    await expect(panel.getByLabel('Response mode')).toHaveValue('both');
    await expect(panel.getByLabel('Required role')).toHaveValue('moderator');
    await expect(panel.getByLabel('Enabled', { exact: true })).not.toBeChecked();
    await panel.locator('textarea').fill('{NOWPLAYING.TITLE} by {nowPlaying.artist} for {DISPLAYNAME}');
    await expect(panel.getByText('Example song by Example artist for TestViewer', { exact: true })).toBeVisible();
    await panel.locator('textarea').fill('界'.repeat(401));
    await expect(panel.getByText('界'.repeat(400), { exact: true })).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Save changes' })).toBeDisabled();
    await panel.locator('textarea').fill('Abandoned edit');
    await panel.getByRole('button', { name: 'Cancel edit' }).click();
    await expect(panel.locator('textarea')).toHaveValue('');
    expect(writes).toBe(0);
    await panel.getByRole('button', { name: 'Edit', exact: true }).click();
    await panel.getByLabel('Cooldown seconds').fill('3601');
    await expect(panel.getByRole('button', { name: 'Save changes' })).toBeDisabled();
    await panel.getByLabel('Cooldown seconds').fill('60');
    await panel.locator('textarea').fill('Updated {user}');
    await panel.getByRole('button', { name: 'Save changes' }).click();
    await expect(panel.getByRole('alert')).toContainText('command save unavailable');
    await expect(panel.locator('textarea')).toHaveValue('Updated {user}');
    await panel.getByRole('button', { name: 'Save changes' }).click();
    await expect(panel.getByRole('button', { name: 'Saving…' })).toBeDisabled();
    await expect(panel.getByRole('button', { name: 'Enable', exact: true })).toBeDisabled();
    await expect(panel.getByRole('button', { name: 'Cancel edit' })).toBeDisabled();
    await expect(panel.locator('textarea')).toBeDisabled();
    releaseSave();
    await expect(panel.getByText('Updated welcome.', { exact: true })).toBeVisible();
    await expect(panel.getByRole('alert')).toContainText('Command operation succeeded. Status/audit refresh failed:');
    await expect(panel.getByText('Updated {user}', { exact: true })).toBeVisible();
    await expect(panel.getByText('alert · both · moderator+ · 60s cooldown')).toBeVisible();
    await panel.getByRole('button', { name: 'Edit', exact: true }).click();
    await panel.getByRole('button', { name: 'Enable', exact: true }).click();
    await expect(panel.getByText('Enabled welcome.', { exact: true })).toBeVisible();
    await expect(panel.getByLabel('Enabled', { exact: true })).toBeChecked();
    await panel.getByRole('button', { name: 'Delete', exact: true }).click();
    await expect(panel.getByText('Deleted welcome.', { exact: true })).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Cancel edit' })).toHaveCount(0);
    await expect(panel.getByText('No bot commands configured yet.')).toBeVisible();
    expect(writes).toBe(2);
  });

  test('edits API variables with retry, pending controls, and successful receipts after refresh failure', async ({ page }) => {
    let releaseSave!: () => void;
    const delayedSave = new Promise<void>((resolve) => { releaseSave = resolve; });
    let writes = 0;
    let deletes = 0;
    let removed = false;
    let auditUnavailable = false;
    let variable = { id: 'variable-edit', name: 'weather', enabled: false, url: 'https://api.example.test/old', responseSelector: 'data.temp', fallback: 'offline', cacheTtlSeconds: 60, timeoutMs: 1500 };
    await page.route('**/api/bot/**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      const method = route.request().method();
      if (path.endsWith('/rules') || (path.endsWith('/audit') && auditUnavailable)) {
        await route.fulfill({ status: 503, body: 'summary unavailable' });
        return;
      }
      if (path.endsWith('/api-variables') && method === 'POST') {
        const payload = route.request().postDataJSON();
        expect(payload.id).toBe('variable-edit');
        expect(payload.enabled).toBe(false);
        expect(payload.responseSelector).toBe('data.temp');
        expect(payload.fallback).toBe('offline');
        writes += 1;
        if (writes === 1) await route.fulfill({ status: 503, body: 'variable save unavailable' });
        else {
          await delayedSave;
          variable = { ...variable, ...payload };
          auditUnavailable = true;
          await route.fulfill({ json: variable });
        }
        return;
      }
      if (path.endsWith('/api-variables/variable-edit') && method === 'DELETE') {
        deletes += 1;
        if (deletes === 1) await route.fulfill({ status: 503, body: 'variable delete unavailable' });
        else { removed = true; await route.fulfill({ status: 204 }); }
        return;
      }
      const responses: Record<string, unknown> = {
        status: { available: true, commandCount: 0, timerCount: 0, counterCount: 0, quoteCount: 0, auditCount: 0 },
        'api-variables': { variables: removed ? [] : [variable] }, commands: { commands: [] }, timers: { timers: [] }, counters: { counters: [] }, quotes: { quotes: [] }, audit: { events: [] },
      };
      await route.fulfill({ json: responses[path.split('/').at(-1)!] ?? {} });
    });
    await page.goto('/dashboard/chat');
    const panel = page.locator('#bot-api-variables');
    await panel.getByRole('button', { name: 'Edit', exact: true }).click();
    await expect(panel.getByLabel('JSON selector')).toHaveValue('data.temp');
    await expect(panel.getByLabel('Fallback')).toHaveValue('offline');
    await expect(panel.getByLabel('Enabled', { exact: true })).not.toBeChecked();
    await panel.getByLabel('HTTPS URL').fill('https://api.example.test/abandoned');
    await panel.getByRole('button', { name: 'Cancel edit' }).click();
    await expect(panel.getByLabel('HTTPS URL')).toHaveValue('');
    expect(writes).toBe(0);
    await panel.getByRole('button', { name: 'Edit', exact: true }).click();
    await panel.getByLabel('TTL seconds').fill('14');
    await expect(panel.getByRole('button', { name: 'Save changes' })).toBeDisabled();
    await panel.getByLabel('TTL seconds').fill('120');
    await panel.getByLabel('Timeout ms').fill('3001');
    await expect(panel.getByRole('button', { name: 'Save changes' })).toBeDisabled();
    await panel.getByLabel('Timeout ms').fill('2000');
    await panel.getByLabel('HTTPS URL').fill('https://api.example.test/new');
    await panel.getByRole('button', { name: 'Save changes' }).click();
    await expect(panel.getByRole('alert')).toContainText('variable save unavailable');
    await expect(panel.getByLabel('HTTPS URL')).toHaveValue('https://api.example.test/new');
    await panel.getByRole('button', { name: 'Save changes' }).click();
    await expect(panel.getByRole('button', { name: 'Saving…' })).toBeDisabled();
    await expect(panel.getByRole('button', { name: 'Delete', exact: true })).toBeDisabled();
    await expect(panel.getByLabel('Enabled', { exact: true })).toBeDisabled();
    releaseSave();
    await expect(panel.getByText('Updated weather.', { exact: true })).toBeVisible();
    await expect(panel.getByRole('alert')).toContainText('API variable operation succeeded. Status/audit refresh failed:');
    await expect(panel.getByText('https://api.example.test/new', { exact: true })).toBeVisible();
    await expect(panel.getByText('status untested · ttl 120s · timeout 2000ms')).toBeVisible();
    await panel.getByRole('button', { name: 'Edit', exact: true }).click();
    await panel.getByRole('button', { name: 'Delete', exact: true }).click();
    await expect(panel.getByRole('alert')).toContainText('variable delete unavailable');
    await expect(panel.getByRole('button', { name: 'Cancel edit' })).toBeVisible();
    await panel.getByRole('button', { name: 'Delete', exact: true }).click();
    await expect(panel.getByText('Deleted weather.', { exact: true })).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Cancel edit' })).toHaveCount(0);
    await expect(panel.getByText('No API variables configured yet.')).toBeVisible();
    expect(writes).toBe(2);
    expect(deletes).toBe(2);
  });

  test('keeps moderation drafts, clears stale dry runs, and displays failed action receipts accurately', async ({ page }) => {
    let releaseAction!: () => void;
    const actionReady = new Promise<void>((resolve) => { releaseAction = resolve; });
    let saves = 0;
    let actions = 0;
    let auditUnavailable = false;
    const rule = { id: 'rule-1', name: 'fixture_rule', enabled: true, enforce: false, pattern: 'spam', matchTarget: 'message', regex: false, caseSensitive: false, action: 'flag', reason: '' };
    await page.route('**/api/bot/**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      const method = route.request().method();
      if (path.endsWith('/rules') && method === 'POST') {
        saves += 1;
        if (saves === 1) await route.fulfill({ status: 503, body: 'rule save unavailable' });
        else await route.fulfill({ json: rule });
        return;
      }
      if (path.endsWith('/rules/rule-1') && method === 'DELETE') { await route.fulfill({ status: 204 }); return; }
      if (path.endsWith('/moderation/dry-run')) {
        await route.fulfill({ json: { matches: [{ ruleName: 'fixture_rule', action: 'flag' }] } });
        return;
      }
      if (path.endsWith('/moderation/action')) {
        actions += 1;
        if (actions === 1) await route.fulfill({ status: 503, body: 'Twitch unavailable' });
        else {
          await actionReady;
          auditUnavailable = true;
          await route.fulfill({ json: { action: 'timeout', result: 'failed', reason: 'missing_scope' } });
        }
        return;
      }
      if (path.endsWith('/audit') && auditUnavailable) { await route.fulfill({ status: 503, body: 'audit unavailable' }); return; }
      const responses: Record<string, unknown> = {
        status: { available: true, commandCount: 0, timerCount: 0, counterCount: 0, quoteCount: 0, auditCount: actions },
        rules: { rules: [] }, commands: { commands: [] }, timers: { timers: [] }, counters: { counters: [] }, quotes: { quotes: [] }, 'api-variables': { variables: [] },
        audit: { events: actions ? [{ id: 'audit-failed', createdAt: '2026-10-06T05:00:00Z', source: 'dashboard', result: 'failed', reason: 'manual_action_failed', trigger: 'timeout' }] : [] },
      };
      await route.fulfill({ json: responses[path.split('/').at(-1)!] ?? {} });
    });
    await page.goto('/dashboard/chat');
    const panel = page.locator('#bot-moderation');
    await panel.getByLabel('Rule name').fill('fixture_rule');
    await panel.getByLabel('Pattern', { exact: true }).fill('spam');
    await panel.getByRole('button', { name: 'Save rule', exact: true }).click();
    await expect(panel.getByRole('alert')).toContainText('rule save unavailable');
    await expect(panel.getByLabel('Rule name')).toHaveValue('fixture_rule');
    await panel.getByRole('button', { name: 'Save rule', exact: true }).click();
    await expect(panel.getByText('Saved fixture_rule.', { exact: true })).toBeVisible();
    await expect(panel.getByText('fixture_rule', { exact: true })).toBeVisible();
    await panel.getByLabel('Message to test').fill('spam');
    await panel.getByRole('button', { name: 'Dry run', exact: true }).click();
    await expect(panel.getByText('fixture_rule:flag', { exact: true })).toBeVisible();
    await panel.getByLabel('Message to test').fill('different message');
    await expect(panel.getByText('fixture_rule:flag', { exact: true })).toHaveCount(0);
    await panel.getByLabel('Target user ID').fill('fixture-user');
    await panel.getByRole('button', { name: 'Run guarded action' }).click();
    await expect(panel.getByRole('alert')).toContainText('Twitch unavailable');
    await expect(panel.getByLabel('Target user ID')).toHaveValue('fixture-user');
    await expect(page.getByText('dashboard · manual_action_failed')).toBeVisible();
    await expect(page.locator('#bot-nuke').getByRole('alert')).toHaveCount(0);
    await panel.getByRole('button', { name: 'Run guarded action' }).click();
    await expect(panel.getByRole('button', { name: 'Running…' })).toBeDisabled();
    await expect(panel.getByLabel('Target user ID')).toBeDisabled();
    releaseAction();
    const receipt = panel.getByText('timeout failed · missing_scope', { exact: true });
    await expect(receipt).toBeVisible();
    await expect(receipt).toHaveClass(/bad/);
    await expect(panel.getByRole('alert')).toContainText('Operation completed. Status/audit refresh failed:');
    await panel.getByRole('button', { name: 'Delete', exact: true }).click();
    await expect(panel.getByText('Deleted fixture_rule.', { exact: true })).toBeVisible();
    await expect(panel.getByText('No moderation rules configured yet.')).toBeVisible();
    expect(saves).toBe(2);
    expect(actions).toBe(2);
  });

  test('invalidates changed nuke plans, blocks duplicate execution, and retains execution and rollback receipts', async ({ page }) => {
    let releaseExecution!: () => void;
    const executionReady = new Promise<void>((resolve) => { releaseExecution = resolve; });
    let plans = 0;
    let executions = 0;
    let rulesReads = 0;
    let auditUnavailable = false;
    const radiation = { id: 'radiation-1', name: 'nuke_window', enabled: true, enforce: true, pattern: 'spam two', matchTarget: 'message', regex: false, caseSensitive: false, action: 'delete', source: 'nuke_radiation', expiresAt: '2099-10-06T06:00:00Z' };
    await page.route('**/api/bot/**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path.endsWith('/nuke/dry-run')) {
        plans += 1;
        const payload = route.request().postDataJSON();
        expect(payload.confirm).toBe(false);
        await route.fulfill({ json: { matches: [{ eventId: 'chat-1', messageId: 'message-1', user: 'fixture', userId: 'fixture-1', message: payload.pattern }], pattern: payload.pattern, action: payload.action } });
        return;
      }
      if (path.endsWith('/nuke/execute')) {
        const payload = route.request().postDataJSON();
        expect(payload.pattern).toBe('spam two');
        expect(payload.confirm).toBe(true);
        executions += 1;
        await executionReady;
        auditUnavailable = true;
        await route.fulfill({ json: { matches: [1, 2, 3].map((id) => ({ eventId: `chat-${id}`, user: 'fixture', message: 'spam two' })), attempted: 1, succeeded: 1, failed: 0, interrupted: true, action: 'delete', radiationRule: radiation } });
        return;
      }
      if (path.endsWith('/nuke/radiation/rollback')) {
        expect(route.request().postDataJSON()).toEqual({ ruleId: 'radiation-1', confirm: true });
        await route.fulfill({ json: { rule: { ...radiation, enabled: false, enforce: false } } });
        return;
      }
      if (path.endsWith('/audit') && auditUnavailable) {
        await route.fulfill({ status: 503, body: 'audit unavailable after operation' });
        return;
      }
      if (path.endsWith('/rules')) rulesReads += 1;
      const responses: Record<string, unknown> = {
        status: { available: true, commandCount: 0, timerCount: 0, counterCount: 0, quoteCount: 0, auditCount: 0 },
        rules: { rules: [] }, commands: { commands: [] }, timers: { timers: [] }, counters: { counters: [] }, quotes: { quotes: [] }, 'api-variables': { variables: [] }, audit: { events: [] },
      };
      await route.fulfill({ json: responses[path.split('/').at(-1)!] ?? {} });
    });
    await page.goto('/dashboard/chat');
    const panel = page.locator('#bot-nuke');
    const confirm = panel.getByLabel('I reviewed this dry-run plan');
    const execute = panel.getByRole('button', { name: 'Execute planned nuke', exact: true });
    await expect(confirm).toBeDisabled();
    await panel.getByLabel('Pattern', { exact: true }).fill('spam one');
    await panel.getByRole('button', { name: 'Plan nuke', exact: true }).click();
    await expect(panel.getByText('Planned matches: 1')).toBeVisible();
    await confirm.check();
    await expect(execute).toBeEnabled();
    await panel.getByLabel('Pattern', { exact: true }).fill('spam two');
    await expect(confirm).not.toBeChecked();
    await expect(confirm).toBeDisabled();
    await expect(panel.getByText('Planned matches: 1')).toHaveCount(0);
    await expect(execute).toBeDisabled();
    await panel.getByRole('button', { name: 'Plan nuke', exact: true }).click();
    await expect(panel.getByText('Planned matches: 1')).toBeVisible();
    await confirm.check();
    const initialRulesReads = rulesReads;
    await execute.click();
    await expect(panel.getByRole('button', { name: 'Executing…' })).toBeDisabled();
    await expect(confirm).not.toBeChecked();
    await expect(confirm).toBeDisabled();
    await expect(panel.getByLabel('Pattern', { exact: true })).toBeDisabled();
    await expect(page.locator('#bot-moderation').getByRole('button', { name: 'Run guarded action' })).toBeDisabled();
    releaseExecution();
    await expect(panel.getByText(/Executed delete: 1\/1 succeeded/)).toBeVisible();
    await expect(panel.getByText(/execution interrupted; 2 not attempted/)).toBeVisible();
    await expect(panel.getByRole('alert')).toContainText('Operation completed. Status/audit refresh failed:');
    await expect(panel.getByRole('button', { name: 'Rollback window' })).toBeEnabled();
    await expect(execute).toBeDisabled();
    await panel.getByRole('button', { name: 'Rollback window' }).click();
    await expect(panel.getByText('Rollback: disabled nuke_window')).toBeVisible();
    await expect(panel.getByText(/Executed delete: 1\/1 succeeded/)).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Rollback window' })).toHaveCount(0);
    expect(plans).toBe(2);
    expect(executions).toBe(1);
    expect(rulesReads).toBe(initialRulesReads);
  });

  test('isolates counter and quote writes, preserves failed drafts, and retains successful receipts', async ({ page }) => {
    let releaseCounter!: () => void;
    const delayedCounter = new Promise<void>((resolve) => { releaseCounter = resolve; });
    let counterWrites = 0;
    let quoteDeletes = 0;
    let auditUnavailable = false;
    let counter: { id: string; name: string; value: number } | null = null;
    let quote: { id: string; text: string; author: string } | null = null;
    await page.route('**/api/bot/**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      const method = route.request().method();
      if (path.endsWith('/counters') && method === 'POST') {
        counterWrites += 1;
        if (counterWrites === 1) {
          await delayedCounter;
          await route.fulfill({ status: 503, body: 'counter write unavailable' });
        } else {
          counter = { id: 'counter-1', ...route.request().postDataJSON() };
          auditUnavailable = true;
          await route.fulfill({ json: counter });
        }
        return;
      }
      if (path.endsWith('/quotes') && method === 'POST') {
        quote = { id: 'quote-1', ...route.request().postDataJSON() };
        await route.fulfill({ json: quote });
        return;
      }
      if (path.endsWith('/quotes/quote-1') && method === 'DELETE') {
        quoteDeletes += 1;
        if (quoteDeletes === 1) await route.fulfill({ status: 503, body: 'quote delete unavailable' });
        else { quote = null; await route.fulfill({ status: 204 }); }
        return;
      }
      if (path.endsWith('/counters/counter-1') && method === 'DELETE') {
        counter = null;
        await route.fulfill({ status: 204 });
        return;
      }
      if (path.endsWith('/quotes') || path.endsWith('/api-variables') || (path.endsWith('/audit') && auditUnavailable)) {
        await route.fulfill({ status: 503, body: 'read temporarily unavailable' });
        return;
      }
      const responses: Record<string, unknown> = {
        status: { available: true, commandCount: 0, timerCount: 0, counterCount: counter ? 1 : 0, quoteCount: quote ? 1 : 0, auditCount: quote ? 1 : 0 },
        counters: { counters: [] }, timers: { timers: [] }, commands: { commands: [] }, rules: { rules: [] },
        audit: { events: quote ? [{ id: 'audit-quote', createdAt: '2026-10-06T05:00:00Z', source: 'dashboard', result: 'allowed', reason: 'quote_configuration_changed', trigger: 'quote_configuration' }] : [] },
      };
      await route.fulfill({ json: responses[path.split('/').at(-1)!] ?? {} });
    });
    await page.goto('/dashboard/chat');
    const panel = page.locator('#bot-community');
    const counters = panel.locator('.desk-sub').filter({ has: page.getByText('Counters', { exact: true }) });
    const quotes = panel.locator('.desk-sub').filter({ has: page.getByText('Quotes', { exact: true }) });
    await expect(quotes.getByText(/Quotes unavailable:/)).toBeVisible();
    await expect(quotes.getByText('No quotes yet.')).toHaveCount(0);
    await counters.getByLabel('Counter name').fill('hype');
    await counters.getByLabel('Value', { exact: true }).fill('7');
    await counters.getByRole('button', { name: 'Save counter', exact: true }).click();
    await expect(counters.getByRole('button', { name: 'Saving counter…' })).toBeDisabled();
    await expect(counters.getByLabel('Counter name')).toBeDisabled();
    await quotes.getByLabel('Quote text').fill('Saved during counter write');
    await quotes.getByLabel('Author').fill('Operator');
    await quotes.getByRole('button', { name: 'Save quote', exact: true }).click();
    await expect(quotes.getByText('Quote saved.', { exact: true })).toBeVisible();
    await expect(quotes.getByText('Saved during counter write', { exact: true })).toBeVisible();
    await expect(page.getByText('dashboard · quote_configuration_changed')).toBeVisible();
    releaseCounter();
    await expect(counters.getByRole('alert')).toContainText('counter write unavailable');
    await expect(counters.getByLabel('Counter name')).toHaveValue('hype');
    await expect(counters.getByLabel('Value', { exact: true })).toHaveValue('7');
    await expect(quotes.getByRole('alert')).toHaveCount(0);
    await counters.getByRole('button', { name: 'Save counter', exact: true }).click();
    await expect(counters.getByText('Counter saved.', { exact: true })).toBeVisible();
    await expect(counters.getByText('hype · 7', { exact: true })).toBeVisible();
    await expect(counters.getByRole('alert')).toContainText('Counter operation succeeded. Status/audit refresh failed:');
    await expect(counters.getByLabel('Counter name')).toHaveValue('');
    await counters.getByRole('button', { name: 'Delete', exact: true }).click();
    await expect(counters.getByText('Counter deleted.', { exact: true })).toBeVisible();
    await expect(counters.getByText('hype · 7', { exact: true })).toHaveCount(0);
    await quotes.getByRole('button', { name: 'Delete', exact: true }).click();
    await expect(quotes.getByRole('alert')).toContainText('quote delete unavailable');
    await expect(quotes.getByText('Saved during counter write', { exact: true })).toBeVisible();
    await quotes.getByRole('button', { name: 'Delete', exact: true }).click();
    await expect(quotes.getByText('Quote deleted.', { exact: true })).toBeVisible();
    await expect(quotes.getByText('Saved during counter write', { exact: true })).toHaveCount(0);
    expect(counterWrites).toBe(2);
    expect(quoteDeletes).toBe(2);
  });

  test('reviews and exports session bot actions with bounded and unavailable audit diagnostics', async ({ page }) => {
    let unavailable = false;
    const session = { id: 'session-report-1', title: 'Community audit rehearsal', kind: 'rehearsal', startedAt: '2026-10-06T05:00:00Z', endedAt: '2026-10-06T05:01:00Z', eventCount: 0 };
    const audit = { total: 3, events: [{ id: 'audit-1', streamSessionId: session.id, createdAt: '2026-10-06T05:00:30Z', source: 'dashboard', actorRole: 'operator', action: 'quote_create', result: 'allowed', reason: 'quote_configuration_changed' }] };
    await page.route('**/api/stream/session**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path.endsWith('/session-report-1')) {
        await route.fulfill({ json: { session, events: [], snapshots: [], botAudit: unavailable ? { events: [], total: 0, loadError: 'session bot audit unavailable' } : audit } });
      } else {
        await route.fulfill({ json: { recent: [session] } });
      }
    });
    await page.goto('/dashboard/sessions');
    await page.getByRole('button', { name: 'Open latest report' }).click();
    const panel = page.getByRole('region', { name: 'Session bot actions' });
    // A named section without an explicit role is exposed as a region.
    await expect(panel.getByText('quote_create', { exact: true })).toBeVisible();
    await expect(panel.getByText('1 of 3 bot actions loaded.')).toBeVisible();
    await expect(panel.getByText('Only the newest 1 of 3 bot actions are loaded. Exports include these loaded rows.')).toBeVisible();

    const jsonDownload = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Export JSON', exact: true }).click();
    const jsonFile = await (await jsonDownload).path();
    expect(jsonFile).not.toBeNull();
    const exported = JSON.parse(await readFile(jsonFile!, 'utf8'));
    expect(exported.botAudit).toEqual(audit);
    const markdownDownload = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Export Markdown', exact: true }).click();
    const markdownFile = await (await markdownDownload).path();
    expect(markdownFile).not.toBeNull();
    const markdown = await readFile(markdownFile!, 'utf8');
    expect(markdown).toContain('## Bot actions');
    expect(markdown).toContain('quote_create');
    expect(markdown).toContain('1 of 3 bot actions exported.');

    unavailable = true;
    await page.getByRole('button', { name: 'Open latest report' }).click();
    await expect(panel.getByText('Bot audit unavailable: session bot audit unavailable')).toBeVisible();
    await expect(panel.getByText('No bot actions recorded for this session.')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Export JSON', exact: true })).toBeEnabled();
  });

  test('previews every overlay in demo mode', async ({ page }) => {
    await page.goto('/overlay/brb?demo=1');
    await expect(page.getByText('Swapping tapes. Chat stays open.')).toBeVisible();
    await page.goto('/overlay/starting?minutes=10');
    await expect(page.getByText('10:00')).toBeVisible();
  });
});
