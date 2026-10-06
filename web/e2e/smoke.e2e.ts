import { expect, test } from '@playwright/test';
import { readFile } from 'node:fs/promises';

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
    let releaseVariables!: () => void;
    const variablesReady = new Promise<void>((resolve) => { releaseVariables = resolve; });
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
      if (path.endsWith('/api-variables')) {
        await variablesReady;
        await route.fulfill({ json: { variables: [] } });
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
    releaseVariables();
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
