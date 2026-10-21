import { expect, test, type Page } from '@playwright/test';
import { createHash } from 'node:crypto';

const actor = '11111111-1111-4111-8111-111111111111';
const tenant = '22222222-2222-4222-8222-222222222222';
const automation = '33333333-3333-4333-8333-333333333333';
const version = '44444444-4444-4444-8444-444444444444';
const older = '55555555-5555-4555-8555-555555555555';
const hash = 'a'.repeat(64);
const at = '2026-10-21T23:00:00Z';
function document(id = version) { return { schemaVersion: 2, tenantId: tenant, automationId: automation, versionId: id, trigger: { id: 'button', kind: 'dashboard' }, capabilities: ['chat.send'], restrictions: [], admission: { groups: [], cooldownMs: 0, perStreamLimit: 0 }, root: 'root', nodes: [{ id: 'root', kind: 'action', capability: 'chat.send', arguments: [{ name: 'text', value: id === older ? '<img src=x onerror=alert(1)>' : 'Fixture version text $(user) <img src=x onerror=alert(1)>' }] }] }; }
const identity = (parts: string[]) => createHash('sha256').update(JSON.stringify(parts)).digest('hex');

async function fixture(page: Page) {
  const state = { role: 'owner', mode: 'ok', workspace: tenant, operations: [] as { operation: string; body: Record<string, unknown> }[], empty: false, chooseCurrent: (id: string) => current.set(automation, id) };
  const docs = new Map<string, Record<string, unknown>>([[version, document()], [older, document(older)]]);
  const current = new Map([[automation, version]]);
  const runs = new Map<string, { runId: string; occurrenceId: string; automationId: string; versionId: string; graphDigest: string; createdAt: string; deadline: string; unchanged: boolean; state: string }>();
  const traces = new Map<string, unknown>();
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
    expect(['list', 'list-versions', 'load', 'validate', 'publish', 'select', 'admit', 'lookup', 'rehearse', 'save-trace', 'load-trace']).toContain(operation);
    if (operation === 'admit') {
      if (state.mode === 'admission-error') return route.fulfill({ status: 409, body: 'PRIVATE POLICY DETAIL' });
      const aid = body.automationId as string; const vid = body.expectedVersionId as string; const request = body.requestId as string;
      if (current.get(aid) !== vid) return route.fulfill({ status: 409 });
      const occurrenceId = identity(['overlaid.engine.operator-occurrence.v1', state.workspace, actor, request]);
      const root = identity(['overlaid.engine.run.v1', state.workspace, occurrenceId, aid, vid, hash, 'execute']);
      const run = { runId: identity(['overlaid.engine.pinned-run.v1', root, hash]), occurrenceId, automationId: aid, versionId: vid, graphDigest: hash, createdAt: at, deadline: '2026-10-21T23:02:00Z', unchanged: false, state: 'admitted_metadata' };
      runs.set(request, run);
      if (state.mode === 'lost-run') return route.abort();
      return route.fulfill({ json: state.mode === 'malformed-run' ? { ...run, runId: 'f'.repeat(64) } : run });
    }
    if (['lookup', 'rehearse', 'save-trace', 'load-trace'].includes(operation)) {
      const run = runs.get(body.requestId as string);
      if (!run || state.mode === 'purged-run') return route.fulfill({ status: 404, body: 'PRIVATE PURGED INPUT' });
      if (operation === 'lookup') return route.fulfill({ json: { ...run, unchanged: true } });
      if (operation === 'load-trace') {
        const trace = traces.get(body.traceId as string);
        return trace && state.mode !== 'purged-trace' ? route.fulfill({ json: trace }) : route.fulfill({ status: 404 });
      }
      const mode = body.mode as string;
      const selected = mode === 'reproduce' ? run.versionId : current.get(run.automationId)!;
      const changed = selected !== run.versionId;
      const graphDigest = changed ? 'c'.repeat(64) : hash; const captureDigest = 'b'.repeat(64);
      const steps = [{ sequence: 1, parent: 0, automationId: run.automationId, versionId: selected, nodeId: 'button', kind: 'admission', outcome: 'checked', reason: changed ? 'filters_unavailable' : 'matched' }];
      if (!changed) steps.push({ sequence: 2, parent: 0, automationId: run.automationId, versionId: selected, nodeId: 'root', kind: 'action', outcome: 'proposed', reason: 'opaque_arguments' });
      const proposals = changed ? [] : [{ sequence: 2, automationId: run.automationId, versionId: selected, nodeId: 'root', declaredCapability: 'chat.send', opaqueArguments: document(selected).nodes[0].arguments }];
      const report = { captureVersion: 1, selectedVersionId: selected, run: { ...run, unchanged: true }, rehearsal: { id: identity(['overlaid.engine.rehearsal.v1', graphDigest, run.graphDigest, run.occurrenceId, mode, captureDigest, '100']), mode, originalGraphDigest: run.graphDigest, graphDigest, captureDigest, clock: run.createdAt, seed: 0, status: changed ? 'blocked' : 'proposed', reason: changed ? 'filters_unavailable' : 'supported_flow', steps, droppedSteps: 0, proposals } };
      if (operation === 'save-trace') {
        if (state.mode === 'trace-error' || body.expectedVersionId !== selected) return route.fulfill({ status: 409, body: 'PRIVATE TRACE CONFLICT' });
        const r = report.rehearsal;
        const document = { formatVersion: 1, tenantId: state.workspace, id: r.id, runId: run.runId, automationId: run.automationId, versionId: selected, mode, captureVersion: 1, originalGraphDigest: run.graphDigest, graphDigest, captureDigest, clock: run.createdAt, seed: 0, status: r.status, reason: r.reason, steps: steps.map(s => ({ Sequence: s.sequence, Parent: s.parent, AutomationID: s.automationId, VersionID: s.versionId, NodeID: s.nodeId, Kind: s.kind, Outcome: s.outcome, Reason: s.reason })), droppedSteps: 0 };
        const saved = { document, digest: createHash('sha256').update('overlaid.engine.operator-trace.v1\0' + JSON.stringify(document)).digest('hex'), createdAt: at, unchanged: false };
        traces.set(r.id, saved);
        if (state.mode === 'lost-trace') return route.abort();
        return route.fulfill({ json: state.mode === 'malformed-trace' ? { ...saved, digest: 'f'.repeat(64) } : saved });
      }
      if (state.mode === 'malformed-report') report.rehearsal.seed = 99;
      return route.fulfill({ json: report });
    }
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
      if (state.mode === 'compare-error') return route.fulfill({ status: 503, body: 'PRIVATE COMPARE FAILURE' });
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

test('root comparison reads a fresh current snapshot and preserves unresolved draft state', async ({ page }) => {
  const state = await fixture(page);
  await openDesk(page);
  await page.getByRole('button', { name: automation, exact: true }).click();
  await page.getByRole('combobox', { name: 'Loaded version' }).selectOption(older);
  await page.getByRole('button', { name: 'Draft from loaded version' }).click();
  const input = page.getByRole('textbox', { name: 'Automation document JSON' });
  const original = await input.inputValue();
  state.mode = 'conflict';
  await page.getByRole('button', { name: 'Publish draft', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Unresolved version intent' })).toBeVisible();
  const savedState = await page.evaluate(() => Object.keys(sessionStorage).filter(k => k.startsWith('overlaid.automation-version-desk')).map(k => sessionStorage.getItem(k)));
  const writes = state.operations.filter(x => ['publish', 'select'].includes(x.operation));
  await page.getByRole('button', { name: 'Compare with current snapshot' }).click();
  const panel = page.getByRole('region', { name: 'Root version comparison', exact: true });
  await expect(panel).toContainText('/nodes/0/arguments/0/value');
  await expect(panel).toContainText('<img src=x onerror=alert(1)>');
  await expect(panel.locator('img')).toHaveCount(0);
  await expect(panel).toContainText('Preset dependency contents are not compared');
  await expect(input).toHaveValue(original);
  await expect(input).toBeDisabled();
  expect(await page.evaluate(() => Object.keys(sessionStorage).filter(k => k.startsWith('overlaid.automation-version-desk')).map(k => sessionStorage.getItem(k)))).toEqual(savedState);
  await panel.scrollIntoViewIfNeeded();
  await page.screenshot({ path: '/tmp/overlaid-version-comparison-desktop.png' });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: '/tmp/overlaid-version-comparison-changes-narrow.png', fullPage: true });
  state.chooseCurrent(older);
  await page.getByRole('button', { name: 'Compare with current snapshot' }).click();
  await expect(panel).toContainText('No root document differences found');
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: '/tmp/overlaid-version-comparison-narrow.png', fullPage: true });
  state.mode = 'compare-error';
  await page.getByRole('button', { name: 'Compare with current snapshot' }).click();
  await expect(page.getByRole('alert')).not.toContainText('PRIVATE');
  await expect(panel).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Unresolved version intent' })).toBeVisible();
  expect(state.operations.filter(x => ['publish', 'select'].includes(x.operation))).toEqual(writes);
});

test('operator desk explicitly admits metadata then reproduces original or current capture', async ({ page }) => {
  const state = await fixture(page);
  await openDesk(page);
  const desk = page.getByRole('region', { name: 'Operator rehearsal', exact: true });
  await expect(desk.getByRole('button', { name: 'Prepare rehearsal occurrence' })).toBeDisabled();
  await page.getByRole('button', { name: automation, exact: true }).click();
  await desk.getByRole('button', { name: 'Prepare rehearsal occurrence' }).click();
  expect(state.operations.filter(x => x.operation === 'admit')).toHaveLength(0);
  await desk.getByRole('button', { name: 'Admit occurrence metadata' }).click();
  await expect(desk.getByText('Retained metadata accepted · no execution')).toBeVisible();
  await desk.getByRole('button', { name: 'Reproduce original capture' }).click();
  const report = desk.getByRole('region', { name: 'Retained rehearsal report' });
  await expect(report).toContainText('Rehearsal: proposed');
  await expect(report).toContainText('$(user)');
  await expect(report).toContainText('<img src=x onerror=alert(1)>');
  await expect(report.locator('img')).toHaveCount(0);
  state.chooseCurrent(older);
  await desk.getByRole('button', { name: 'Rehearse current configuration' }).click();
  await expect(report).toContainText('Rehearsal: blocked');
  await expect(report).toContainText('filters_unavailable');
  await expect(report).toContainText(older);
  await desk.getByRole('button', { name: 'Reproduce original capture' }).click();
  await expect(report).toContainText('Rehearsal: proposed');
  state.mode = 'malformed-report';
  await desk.getByRole('button', { name: 'Reproduce original capture' }).click();
  await expect(desk.getByRole('alert')).toContainText('Invalid retained rehearsal metadata');
  await expect(report).toHaveCount(0);
  expect(state.operations.filter(x => x.operation === 'admit')).toHaveLength(1);
  expect(state.operations.filter(x => ['publish', 'select', 'save-trace'].includes(x.operation))).toHaveLength(0);
});

for (const mode of ['lost-run', 'malformed-run', 'admission-error']) test(`operator ${mode} preserves request through reload without re-admission`, async ({ page }) => {
  const state = await fixture(page);
  await openDesk(page);
  await page.getByRole('button', { name: automation, exact: true }).click();
  const desk = page.getByRole('region', { name: 'Operator rehearsal', exact: true });
  await desk.getByRole('button', { name: 'Prepare rehearsal occurrence' }).click();
  state.mode = mode;
  await desk.getByRole('button', { name: 'Admit occurrence metadata' }).click();
  await expect(desk.getByRole('alert')).toBeVisible();
  await expect(desk.getByRole('alert')).not.toContainText('PRIVATE');
  const sent = state.operations.find(x => x.operation === 'admit')!;
  await page.reload();
  await expect(desk.getByText(sent.body.requestId as string, { exact: true })).toBeVisible();
  await expect(desk.getByRole('button', { name: 'Admit occurrence metadata' })).toBeDisabled();
  await desk.getByRole('button', { name: 'Check original occurrence' }).click();
  if (mode === 'admission-error') await expect(desk.getByRole('alert')).toContainText('(404)');
  else await expect(desk.getByText('Retained metadata accepted · no execution')).toBeVisible();
  state.mode = 'purged-run';
  await desk.getByRole('button', { name: 'Check original occurrence' }).click();
  await expect(desk.getByRole('alert')).toContainText('(404)');
  await expect(desk.getByRole('button', { name: 'Reproduce original capture' })).toHaveCount(0);
  expect(state.operations.filter(x => x.operation === 'admit')).toEqual([sent]);
  expect(state.operations.filter(x => x.operation === 'lookup').every(x => x.body.requestId === sent.body.requestId)).toBe(true);
});

test('prepared operator intent needs durable storage and stays in its initiating workspace', async ({ page }) => {
  const state = await fixture(page);
  await openDesk(page);
  await page.getByRole('button', { name: automation, exact: true }).click();
  const desk = page.getByRole('region', { name: 'Operator rehearsal', exact: true });
  await desk.getByRole('button', { name: 'Prepare rehearsal occurrence' }).click();
  await page.reload();
  await expect(desk.getByText('Prepared; no admission attempt sent', { exact: true })).toBeVisible();
  await page.evaluate(() => { Storage.prototype.setItem = () => { throw new Error('fixture quota'); }; });
  await desk.getByRole('button', { name: 'Admit occurrence metadata' }).click();
  await expect(desk.getByRole('alert')).toContainText('storage is unavailable');
  expect(state.operations.filter(x => x.operation === 'admit')).toHaveLength(0);
  state.workspace = actor;
  await page.reload();
  await expect(desk.getByText('Request UUID', { exact: true })).toHaveCount(0);
  state.workspace = tenant;
  await page.reload();
  await expect(desk.getByText('Prepared; no admission attempt sent', { exact: true })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await desk.getByRole('button', { name: 'Admit occurrence metadata' }).focus();
  await page.keyboard.press('Enter');
  await expect(desk.getByText('Retained metadata accepted · no execution')).toBeVisible();
  await desk.getByRole('button', { name: 'Reproduce original capture' }).click();
  await expect(desk.getByRole('region', { name: 'Retained rehearsal report' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: '/tmp/overlaid-rehearsal-desk-narrow.png', fullPage: true });
  await desk.screenshot({ path: '/tmp/overlaid-rehearsal-desk-panel.png' });
  await page.evaluate(() => { const key = Object.keys(sessionStorage).find(k => k.endsWith('.operator-occurrence')); if (key) sessionStorage.setItem(key, '{'); });
  await page.reload();
  await expect(desk.getByRole('alert')).toContainText('unreadable');
  await expect(desk.getByRole('button', { name: 'Prepare rehearsal occurrence' })).toBeDisabled();
});

test('a late rehearsal cannot repaint a different workspace', async ({ page }) => {
  const state = await fixture(page);
  await openDesk(page);
  await page.getByRole('button', { name: automation, exact: true }).click();
  const desk = page.getByRole('region', { name: 'Operator rehearsal', exact: true });
  await desk.getByRole('button', { name: 'Prepare rehearsal occurrence' }).click();
  await desk.getByRole('button', { name: 'Admit occurrence metadata' }).click();
  await expect(desk.getByText('Retained metadata accepted · no execution')).toBeVisible();
  let entered = false;
  let release!: () => void;
  const barrier = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/automations/rehearse', async route => { entered = true; await barrier; await route.fallback().catch(() => {}); });
  await desk.getByRole('button', { name: 'Reproduce original capture' }).click();
  await expect.poll(() => entered).toBe(true);
  state.workspace = actor;
  await page.goto('/dashboard/automations');
  await expect(page.getByRole('button', { name: 'New automation draft' })).toBeEnabled();
  release();
  await expect(desk.getByRole('region', { name: 'Retained rehearsal report' })).toHaveCount(0);
  await expect(desk.getByText('Request UUID', { exact: true })).toHaveCount(0);
  expect(state.operations.filter(x => x.operation === 'admit')).toHaveLength(1);
});

async function rehearseForTrace(page: Page) {
  await openDesk(page);
  await page.getByRole('button', { name: automation, exact: true }).click();
  await page.getByRole('button', { name: 'Prepare rehearsal occurrence' }).click();
  await page.getByRole('button', { name: 'Admit occurrence metadata' }).click();
  await expect(page.getByText('Retained metadata accepted · no execution')).toBeVisible();
  await page.getByRole('button', { name: 'Reproduce original capture' }).click();
  await expect(page.getByRole('region', { name: 'Retained rehearsal report' })).toBeVisible();
}

test('saves metadata separately and loads the original trace after pointer and source UI changes', async ({ page }) => {
  const state = await fixture(page);
  await rehearseForTrace(page);
  await page.getByRole('button', { name: 'Save rehearsal metadata trace' }).click();
  const saved = page.getByRole('region', { name: 'Verified saved trace' });
  await expect(saved).toContainText('Saved metadata trace: proposed');
  await expect(saved).not.toContainText('Fixture version text');
  await expect(saved).not.toContainText('$(user)');
  expect(state.operations.filter(x => x.operation === 'save-trace')).toHaveLength(1);
  expect(Object.keys(state.operations.find(x => x.operation === 'save-trace')!.body).sort()).toEqual(['expectedVersionId', 'mode', 'requestId']);
  const reference = await page.evaluate(() => sessionStorage.getItem(Object.keys(sessionStorage).find(k => k.endsWith('.operator-trace'))!));
  expect(reference).not.toContain('steps'); expect(reference).not.toContain('arguments'); expect(reference).not.toContain('Fixture');
  page.on('dialog', dialog => dialog.accept());
  await page.getByRole('button', { name: 'Acknowledge and clear rehearsal intent' }).click();
  state.chooseCurrent(older);
  await page.reload();
  await expect(saved).toHaveCount(0);
  const before = state.operations.length;
  await page.getByRole('button', { name: 'Check original saved trace' }).click();
  await expect(saved).toContainText(version);
  expect(state.operations.slice(before).map(x => x.operation)).toEqual(['lookup', 'load-trace']);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: '/tmp/overlaid-trace-desk-narrow.png', fullPage: true });
  await saved.screenshot({ path: '/tmp/overlaid-trace-desk-panel.png' });
  state.mode = 'purged-trace';
  await page.getByRole('button', { name: 'Check original saved trace' }).click();
  await expect(saved).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Saved rehearsal trace', exact: true }).getByRole('alert')).toContainText('(404)');
  expect(state.operations.filter(x => x.operation === 'save-trace')).toHaveLength(1);
});

for (const mode of ['lost-trace', 'malformed-trace', 'trace-error']) test(`trace ${mode} retains a single save identity through reload`, async ({ page }) => {
  const state = await fixture(page);
  await rehearseForTrace(page);
  state.mode = mode;
  await page.getByRole('button', { name: 'Save rehearsal metadata trace' }).click();
  const desk = page.getByRole('region', { name: 'Saved rehearsal trace', exact: true });
  await expect(desk.getByRole('alert')).toBeVisible();
  await expect(desk.getByRole('alert')).not.toContainText('PRIVATE');
  const sent = state.operations.find(x => x.operation === 'save-trace')!;
  await page.reload();
  await expect(desk.getByText(sent.body.requestId as string, { exact: true })).toBeVisible();
  await expect(desk.getByRole('button', { name: 'Save rehearsal metadata trace' })).toBeDisabled();
  await desk.getByRole('button', { name: 'Check original saved trace' }).click();
  if (mode === 'trace-error') await expect(desk.getByRole('alert')).toContainText('(404)');
  else await expect(page.getByRole('region', { name: 'Verified saved trace' })).toBeVisible();
  state.mode = 'purged-run';
  await desk.getByRole('button', { name: 'Check original saved trace' }).click();
  await expect(desk.getByRole('alert')).toContainText('(404)');
  await expect(page.getByRole('region', { name: 'Verified saved trace' })).toHaveCount(0);
  expect(state.operations.filter(x => x.operation === 'save-trace')).toEqual([sent]);
});

test('trace storage refusal and corrupt references block writes and stay scoped', async ({ page }) => {
  const state = await fixture(page);
  await rehearseForTrace(page);
  await page.evaluate(() => { Storage.prototype.setItem = () => { throw new Error('fixture quota'); }; });
  await page.getByRole('button', { name: 'Save rehearsal metadata trace' }).click();
  const desk = page.getByRole('region', { name: 'Saved rehearsal trace', exact: true });
  await expect(desk.getByRole('alert')).toContainText('storage is unavailable');
  expect(state.operations.filter(x => x.operation === 'save-trace')).toHaveLength(0);
  await page.reload();
  await page.evaluate(() => { const key = Object.keys(sessionStorage).find(k => k.endsWith('.operator-occurrence'))!; sessionStorage.setItem(key.replace('.operator-occurrence', '.operator-trace'), '{'); });
  await page.reload();
  await expect(desk.getByRole('alert')).toContainText('unreadable');
  state.workspace = actor;
  await page.reload();
  await expect(desk.getByRole('alert')).toHaveCount(0);
  state.workspace = tenant;
  await page.reload();
  await expect(desk.getByRole('alert')).toContainText('unreadable');
  expect(state.operations.filter(x => x.operation === 'save-trace')).toHaveLength(0);
});

test('late saved trace cannot repaint a departed workspace', async ({ page }) => {
  const state = await fixture(page);
  await rehearseForTrace(page);
  let entered = false;
  let release!: () => void;
  const barrier = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/automations/save-trace', async route => { entered = true; await barrier; await route.fallback().catch(() => {}); });
  await page.getByRole('button', { name: 'Save rehearsal metadata trace' }).click();
  await expect.poll(() => entered).toBe(true);
  state.workspace = actor;
  await page.goto('/dashboard/automations');
  await expect(page.getByRole('button', { name: 'New automation draft' })).toBeEnabled();
  release();
  await expect(page.getByRole('region', { name: 'Verified saved trace' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Check original saved trace' })).toHaveCount(0);
});

test('saved current-config metadata keeps its selected graph and refuses malformed later reads', async ({ page }) => {
  const state = await fixture(page);
  await rehearseForTrace(page);
  state.chooseCurrent(older);
  await page.getByRole('button', { name: 'Rehearse current configuration' }).click();
  await expect(page.getByRole('region', { name: 'Retained rehearsal report' })).toContainText('Rehearsal: blocked');
  await page.getByRole('button', { name: 'Save rehearsal metadata trace' }).click();
  const saved = page.getByRole('region', { name: 'Verified saved trace' });
  await expect(saved).toContainText('current_config');
  await expect(saved).toContainText(older);
  expect(state.operations.find(x => x.operation === 'save-trace')!.body.expectedVersionId).toBe(older);
  state.chooseCurrent(version);
  await page.reload();
  await page.getByRole('button', { name: 'Check original saved trace' }).click();
  await expect(saved).toContainText(older);
  await expect(saved).toContainText('blocked');
  await page.route('**/automations/load-trace', route => route.fulfill({ json: { document: { privateBody: 'PRIVATE WRONG TRACE' } } }));
  await page.getByRole('button', { name: 'Check original saved trace' }).click();
  await expect(saved).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Saved rehearsal trace', exact: true }).getByRole('alert')).toContainText('Invalid saved trace metadata');
  await expect(page.locator('body')).not.toContainText('PRIVATE WRONG TRACE');
  expect(state.operations.filter(x => x.operation === 'save-trace')).toHaveLength(1);
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.screenshot({ path: '/tmp/overlaid-trace-desk-error-desktop.png', fullPage: true });
});
