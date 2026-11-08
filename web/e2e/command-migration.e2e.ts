import { expect, test, type Page } from '@playwright/test';

const actor = '11111111-1111-4111-8111-111111111111';
const tenant = '22222222-2222-4222-8222-222222222222';
const other = '99999999-9999-4999-8999-999999999999';
const hash = 'a'.repeat(64);
const command = { id: 'legacy-command', name: 'hello', enabled: true, action: 'ticker', responseMode: 'both', roleRequired: 'supporter', cooldownSeconds: 37, snapshot: hash, kind: 'template' };
const editorial = { ...command, id: 'editorial-command', name: 'current', action: 'newsroom_current', responseMode: 'chat', kind: 'editorial' };
const source = 'Hi {user} <img src=x onerror=alert(1)>';

async function fixture(page: Page) {
  const state = { workspace: tenant, role: 'owner', mode: 'ok', snapshot: hash, operations: [] as { operation: string; body: Record<string, unknown> }[], finish: null as (() => void) | null };
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/account') return route.fulfill({ json: { authenticated: true, deploymentMode: 'hosted', accountId: actor, activeTenantId: state.workspace, csrfToken: 'fixture-csrf', tenants: [{ id: state.workspace, name: state.workspace === tenant ? 'Fixture commands' : 'Other workspace', role: state.role, slug: 'fixture', plan: 'free', retentionDays: 30 }] } });
    if (!path.includes('/automations/')) return route.fulfill({ status: 503 });
    const operation = path.split('/').at(-1)!;
    const body = route.request().postDataJSON() as Record<string, unknown>;
    state.operations.push({ operation, body });
    expect(route.request().method()).toBe('POST');
    expect(route.request().headers()['x-overlaid-csrf']).toBe('fixture-csrf');
    expect(route.request().headers()['x-overlay-operator-secret']).toBeUndefined();
    expect(['list', 'list-command-migration', 'preview-command-migration']).toContain(operation);
    if (operation === 'list') return route.fulfill({ json: { automations: [] } });
    if (operation === 'list-command-migration') return route.fulfill({ json: { state: 'command_catalogue_only', commands: state.workspace === tenant ? [editorial, { ...command, snapshot: state.snapshot }] : [] } });
    expect(Object.keys(body).sort()).toEqual(['commandId', 'snapshot', 'values']);
    expect(body.commandId).toBe(command.id);
    if (state.mode === 'stale') return route.fulfill({ status: 409, body: 'PRIVATE SAVED RESPONSE' });
    const values = body.values as { displayName: string; user: string };
    const output = `Hi ${values.displayName || values.user} <img src=x onerror=alert(1)>`;
    const response = { state: 'command_preview_only', status: 'template_review', command: { ...command, snapshot: state.mode === 'malformed' ? 'f'.repeat(64) : state.snapshot }, template: { state: 'preview_only', originalSyntax: 1, proposedSyntax: 2, originalSource: source, proposedSource: 'Hi $(user) <img src=x onerror=alert(1)>', legacyOutput: output, proposedOutput: output, different: false, warnings: [{ code: 'output_policy', start: 0, end: source.length }], diagnostics: [] } };
    if (state.mode === 'hold') await new Promise<void>(resolve => { state.finish = resolve; });
    try { return await route.fulfill({ json: response }); } catch { /* Scope departure may abort this fixture request. */ }
  });
  await page.route('**/readyz', route => route.fulfill({ json: { status: 'ok', checks: [] } }));
  return state;
}

async function openSaved(page: Page) {
  await page.goto('/dashboard/automations');
  const desk = page.getByRole('region', { name: 'Saved command migration preview', exact: true });
  await desk.getByRole('button', { name: 'Load saved commands', exact: true }).click();
  await desk.getByRole('button', { name: '!hello', exact: true }).click();
  return desk;
}

test('compares the canonical saved template and keeps editorial actions separate', async ({ page }) => {
  const state = await fixture(page);
  const desk = await openSaved(page);
  const selected = desk.getByRole('region', { name: 'Selected command snapshot' });
  await expect(selected).toContainText('supporter');
  await expect(selected).toContainText('37');
  await expect(selected.getByRole('textbox', { name: /Legacy template/ })).toHaveCount(0);
  await selected.getByRole('textbox', { name: 'Display name', exact: true }).fill('Viewer');
  await selected.getByRole('button', { name: 'Preview conversion', exact: true }).click();
  const report = selected.getByRole('region', { name: 'Conversion preview result' });
  await expect(report).toContainText('Hi Viewer <img src=x onerror=alert(1)>');
  await expect(report).toContainText('Original legacy template');
  await expect(page.locator('img[src=x]')).toHaveCount(0);
  await expect(selected.getByRole('button', { name: /Save|Publish|Select.*current/ })).toHaveCount(0);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.setViewportSize({ width: 320, height: 780 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await desk.getByRole('button', { name: '!current', exact: true }).focus();
  await page.keyboard.press('Enter');
  await expect(selected).toContainText('separate editorial migration contract');
  await expect(selected.getByRole('button', { name: 'Preview conversion', exact: true })).toHaveCount(0);
  expect(state.operations.filter(x => x.operation === 'preview-command-migration')).toHaveLength(1);
});

test('requires a refreshed selection after stale or malformed preview metadata', async ({ page }) => {
  const state = await fixture(page);
  const desk = await openSaved(page);
  state.mode = 'stale';
  await desk.getByRole('button', { name: 'Preview conversion', exact: true }).click();
  await expect(desk.getByRole('alert')).toContainText('changed or was removed');
  await expect(desk.getByRole('alert')).not.toContainText('PRIVATE');
  expect(state.operations.filter(x => x.operation === 'preview-command-migration')).toHaveLength(1);
  state.mode = 'ok'; state.snapshot = 'b'.repeat(64);
  await desk.getByRole('button', { name: 'Load saved commands', exact: true }).click();
  await expect(desk.getByRole('region', { name: 'Selected command snapshot' })).toHaveCount(0);
  await desk.getByRole('button', { name: '!hello', exact: true }).click();
  state.mode = 'malformed';
  await desk.getByRole('button', { name: 'Preview conversion', exact: true }).click();
  await expect(desk.getByRole('alert')).toContainText('Conversion preview failed');
  await expect(desk.getByRole('region', { name: 'Conversion preview result' })).toHaveCount(0);
  const requests = state.operations.filter(x => x.operation === 'preview-command-migration');
  expect(requests).toHaveLength(2);
  expect(requests[1].body.snapshot).toBe(state.snapshot);
});

test('input edits and workspace departure fence late read results', async ({ page }) => {
  const state = await fixture(page);
  const desk = await openSaved(page);
  state.mode = 'hold';
  await desk.getByRole('button', { name: 'Preview conversion', exact: true }).click();
  await expect.poll(() => state.finish !== null).toBe(true);
  await desk.getByRole('textbox', { name: 'Display name', exact: true }).fill('Changed sample');
  state.finish!();
  await expect(desk.getByRole('region', { name: 'Conversion preview result' })).toHaveCount(0);
  state.finish = null;
  await desk.getByRole('button', { name: 'Preview conversion', exact: true }).click();
  await expect.poll(() => state.finish !== null).toBe(true);
  const finish = state.finish!;
  state.workspace = other;
  await page.goto('/dashboard/automations');
  finish();
  await expect(page.getByRole('main')).toContainText('Other workspace');
  await expect(page.getByRole('region', { name: 'Selected command snapshot' })).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Conversion preview result' })).toHaveCount(0);
  state.role = 'moderator';
  await page.reload();
  await expect(page.getByRole('main')).toContainText('Configure permission is required');
  await expect(page.getByRole('button', { name: 'Load saved commands' })).toHaveCount(0);
});
