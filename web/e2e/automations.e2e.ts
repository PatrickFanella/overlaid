import { expect, test, type Page } from '@playwright/test';

const actor = '11111111-1111-4111-8111-111111111111';
const tenant = '22222222-2222-4222-8222-222222222222';
const automation = '33333333-3333-4333-8333-333333333333';
const version = '44444444-4444-4444-8444-444444444444';
const older = '55555555-5555-4555-8555-555555555555';
const hash = 'a'.repeat(64);
const at = '2026-10-21T23:00:00Z';
function document(id = version) { return { schemaVersion: 2, tenantId: tenant, automationId: automation, versionId: id, trigger: { id: 'button', kind: 'dashboard' }, capabilities: ['chat.send'], restrictions: [], admission: { groups: [], cooldownMs: 0, perStreamLimit: 0 }, root: 'root', nodes: [{ id: 'root', kind: 'action', capability: 'chat.send', arguments: [{ name: 'text', value: 'Fixture version text' }] }] }; }

async function fixture(page: Page) {
  const state = { role: 'owner', mode: 'ok', workspace: tenant, operations: [] as { operation: string; body: Record<string, unknown> }[], empty: false };
  const docs = new Map<string, Record<string, unknown>>([[version, document()], [older, document(older)]]);
  const current = new Map([[automation, version]]);
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/account') return route.fulfill({ json: { authenticated: true, deploymentMode: 'hosted', accountId: actor, activeTenantId: state.workspace, csrfToken: 'fixture-csrf', tenants: [{ id: state.workspace, name: 'Fixture workspace', role: state.role, slug: 'fixture', plan: 'free', retentionDays: 30 }] } });
    if (!path.includes('/automations/')) return route.fulfill({ status: 503, body: 'PRIVATE STATUS' });
    const operation = path.split('/').at(-1)!;
    const body = route.request().postDataJSON() as Record<string, unknown>;
    state.operations.push({ operation, body });
    expect(route.request().method()).toBe('POST');
    expect(route.request().headers()['x-overlaid-csrf']).toBe('fixture-csrf');
    expect(route.request().headers()['x-overlay-operator-secret']).toBeUndefined();
    expect(['list', 'list-versions', 'load', 'validate', 'publish', 'select']).toContain(operation);
    if (operation === 'list') {
      if (state.mode === 'error') return route.fulfill({ status: 503, body: 'PRIVATE PROVIDER TEXT' });
      if (state.mode === 'bad-list') return route.fulfill({ json: { automations: [{ automationId: 'PRIVATE MALFORMED' }] } });
      return route.fulfill({ json: { automations: state.empty ? [] : [...current].map(([automationId, currentVersionId]) => ({ automationId, currentVersionId, digest: hash, versionCount: [...docs.values()].filter(d => d.automationId === automationId).length, updatedAt: at, versionCreatedAt: at })) } });
    }
    const aid = body.automationId as string;
    if (operation === 'list-versions') {
      if (!current.has(aid)) return route.fulfill({ status: 404 });
      return route.fulfill({ json: { automationId: aid, currentVersionId: current.get(aid), versions: [...docs].filter(([, d]) => d.automationId === aid).map(([versionId]) => ({ versionId, digest: hash, createdAt: at })) } });
    }
    if (operation === 'load') {
      const d = docs.get(body.versionId as string);
      if (!d) return route.fulfill({ status: 404 });
      return route.fulfill({ json: { automationId: aid, versionId: body.versionId, graphDigest: hash, versions: [d] } });
    }
    if (operation === 'validate') {
      const d = body.document as Record<string, unknown>;
      return route.fulfill({ json: { validation: 'structure_only', automationId: d.automationId, versionId: d.versionId, digest: hash } });
    }
    if (state.mode === 'conflict') return route.fulfill({ status: 409, body: 'PRIVATE CONFLICT BODY' });
    if (operation === 'publish') {
      const d = body.document as Record<string, unknown>;
      docs.set(d.versionId as string, d); current.set(d.automationId as string, d.versionId as string);
      if (state.mode === 'lost') return route.abort();
      return route.fulfill({ json: { automationId: d.automationId, versionId: d.versionId, digest: state.mode === 'malformed' ? 'b'.repeat(64) : hash, graphDigest: hash, createdAt: at, unchanged: false } });
    }
    if (operation === 'select') {
      current.set(aid, body.versionId as string);
      if (state.mode === 'lost') return route.abort();
      return route.fulfill({ json: { automationId: aid, versionId: body.versionId, digest: hash, graphDigest: hash, createdAt: at, unchanged: false } });
    }
    return route.fulfill({ status: 404 });
  });
  await page.route('**/readyz', route => route.fulfill({ json: { status: 'ok', checks: [] } }));
  return state;
}
const openDesk = async (page: Page) => { await page.goto('/dashboard/automations'); await expect(page.getByRole('button', { name: 'New automation draft' })).toBeEnabled(); };

test('version desk discovers, edits, validates, publishes and selects without effects', async ({ page }) => {
  const state = await fixture(page);
  await openDesk(page);
  await page.getByRole('button', { name: automation, exact: true }).click();
  await expect(page.getByRole('combobox', { name: 'Loaded version' })).toHaveValue(version);
  await page.getByRole('button', { name: 'Draft from loaded version' }).click();
  const input = page.getByRole('textbox', { name: 'Automation document JSON' });
  const draft = JSON.parse(await input.inputValue());
  expect(draft.versionId).not.toBe(version);
  await input.fill(JSON.stringify({ ...draft, root: 'root' }, null, 2));
  await page.screenshot({ path: '/tmp/overlaid-version-desk-desktop.png', fullPage: true });
  await page.getByRole('button', { name: 'Validate draft', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Structure valid' })).toBeVisible();
  await page.getByRole('button', { name: 'Publish draft', exact: true }).click();
  await expect(page.getByText('Version published.', { exact: false })).toBeVisible();
  expect(state.operations.filter(x => x.operation === 'publish')).toHaveLength(1);
  expect(state.operations.find(x => x.operation === 'publish')!.body.expectedCurrentVersionId).toBe(version);
  await page.getByRole('button', { name: automation, exact: true }).click();
  await page.getByRole('combobox', { name: 'Loaded version' }).selectOption(older);
  await page.getByRole('button', { name: 'Select loaded version as current' }).click();
  await expect(page.getByText('Current version selected.', { exact: false })).toBeVisible();
  expect(state.operations.filter(x => x.operation === 'select')).toHaveLength(1);
});

for (const mode of ['lost', 'malformed', 'conflict']) test(`retains ${mode} publication through reload and checks only original state`, async ({ page }) => {
  const state = await fixture(page); state.mode = mode;
  await openDesk(page);
  await page.getByRole('button', { name: 'New automation draft' }).click();
  const input = page.getByRole('textbox', { name: 'Automation document JSON' });
  const original = await input.inputValue();
  await page.getByRole('button', { name: 'Publish draft', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Unresolved version intent' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Publish draft', exact: true })).toBeDisabled();
  await expect(page.getByRole('alert')).not.toContainText('PRIVATE');
  const first = state.operations.find(x => x.operation === 'publish')!;
  await page.reload();
  await expect(input).toHaveValue(original);
  await expect(input).toBeDisabled();
  await page.getByRole('button', { name: 'Check saved state' }).click();
  if (mode === 'conflict') {
    await expect(page.getByRole('region', { name: 'Unresolved version intent' })).toBeVisible();
    await expect(input).toHaveValue(original);
  } else await expect(page.getByRole('region', { name: 'Unresolved version intent' })).toHaveCount(0);
  expect(state.operations.filter(x => x.operation === 'publish')).toEqual([first]);
});

test('permission, corrupt storage, narrow layout, keyboard and readable error states', async ({ page }) => {
  const state = await fixture(page); state.role = 'moderator';
  await page.goto('/dashboard/automations');
  await expect(page.getByText('Configure permission is required', { exact: false })).toBeVisible();
  expect(state.operations).toHaveLength(0);
  state.role = 'owner'; state.empty = true;
  await openDesk(page);
  await expect(page.getByText('No automations are saved', { exact: false })).toBeVisible();
  state.mode = 'error';
  await page.getByRole('button', { name: 'Refresh catalogue' }).click();
  await expect(page.getByRole('alert')).not.toContainText('PRIVATE');
  state.mode = 'ok';
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Refresh catalogue' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('button', { name: 'New automation draft' })).toBeEnabled();
  await page.getByRole('button', { name: 'New automation draft' }).click();
  await expect(page.getByRole('textbox', { name: 'Automation document JSON' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: '/tmp/overlaid-version-desk-narrow.png', fullPage: true });
  await page.evaluate(() => { const key = Object.keys(sessionStorage).find(k => k.startsWith('overlaid.automation-version-desk')); if (key) sessionStorage.setItem(key, '{'); });
  await page.reload();
  await expect(page.getByRole('alert')).toContainText('unreadable');
  await expect(page.getByRole('button', { name: 'New automation draft' })).toBeDisabled();
});

test('private draft does not follow another account workspace', async ({ page }) => {
  const state = await fixture(page);
  await openDesk(page);
  await page.getByRole('button', { name: 'New automation draft' }).click();
  const input = page.getByRole('textbox', { name: 'Automation document JSON' });
  const original = await input.inputValue();
  state.workspace = actor;
  await openDesk(page);
  await expect(input).toHaveCount(0);
  state.workspace = tenant;
  await page.reload();
  await expect(input).toHaveValue(original);
  expect(state.operations.filter(x => ['publish', 'select'].includes(x.operation))).toHaveLength(0);
});

test('unresolved selection survives reload without issuing a replacement write', async ({ page }) => {
  const state = await fixture(page);
  await openDesk(page);
  await page.getByRole('button', { name: automation, exact: true }).click();
  await page.getByRole('combobox', { name: 'Loaded version' }).selectOption(older);
  state.mode = 'lost';
  await page.getByRole('button', { name: 'Select loaded version as current' }).click();
  await expect(page.getByRole('region', { name: 'Unresolved version intent' })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('button', { name: 'Check saved state' })).toBeEnabled();
  await page.getByRole('button', { name: 'Check saved state' }).click();
  await expect(page.getByText('The requested version is current', { exact: false })).toBeVisible();
  expect(state.operations.filter(x => x.operation === 'select')).toHaveLength(1);
});

test('tab-storage failure prevents the first publication POST', async ({ page }) => {
  const state = await fixture(page);
  await openDesk(page);
  await page.getByRole('button', { name: 'New automation draft' }).click();
  await page.evaluate(() => { Storage.prototype.setItem = () => { throw new Error('fixture quota'); }; });
  await page.getByRole('button', { name: 'Publish draft', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Tab storage is unavailable');
  expect(state.operations.filter(x => x.operation === 'publish')).toHaveLength(0);
  await expect(page.getByRole('textbox', { name: 'Automation document JSON' })).toBeVisible();
});

test('late graph from a departed workspace cannot repaint the new desk', async ({ page }) => {
  const state = await fixture(page);
  let release!: () => void;
  const barrier = new Promise<void>(resolve => { release = resolve; });
  let entered = false;
  await page.route('**/automations/load', async route => {
    entered = true;
    await barrier;
    await route.fulfill({ json: { automationId: automation, versionId: version, graphDigest: hash, versions: [{ ...document(), privateMarker: 'OBSOLETE WORKSPACE TEXT' }] } }).catch(() => {});
  });
  await openDesk(page);
  await page.getByRole('button', { name: automation, exact: true }).click();
  await expect.poll(() => entered).toBe(true);
  state.workspace = actor;
  await page.goto('/dashboard/automations');
  await expect(page.getByRole('button', { name: 'New automation draft' })).toBeEnabled();
  release();
  await expect(page.getByRole('region', { name: 'Saved version', exact: true })).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText('OBSOLETE WORKSPACE TEXT');
  expect(state.operations.filter(x => ['publish', 'select'].includes(x.operation))).toHaveLength(0);
});
