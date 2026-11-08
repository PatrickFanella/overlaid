import { expect, test, type Page } from '@playwright/test';
import type { OBSProposal, OBSRule } from '../src/lib/obsFallback';

const account = '11111111-1111-4111-8111-111111111111';
const tenant = '22222222-2222-4222-8222-222222222222';
const other = '55555555-5555-4555-8555-555555555555';
const automation = '33333333-3333-4333-8333-333333333333';
const version = '44444444-4444-4444-8444-444444444444';
const hash = 'a'.repeat(64);

async function fixture(page: Page, saved = true) {
  let release = () => {};
  const blocked = new Promise<void>(resolve => { release = resolve; });
  const state = { mode: 'ok', workspace: tenant, role: 'owner', available: true, rule: null as OBSRule | null, proposal: null as OBSProposal | null, calls: [] as { op: string; body: Record<string, unknown> }[], effects: 0, release };
  function makeProposal(offset = 0) {
    const observedAt = new Date(Date.now() + offset).toISOString();
    state.proposal = { id: 'b'.repeat(64), versionId: state.rule!.versionId, graphDigest: hash, observationId: 'c'.repeat(64), condition: 'missing', reason: 'input_missing', scene: state.rule!.scene, observedAt, deadline: new Date(Date.parse(observedAt) + 10000).toISOString(), state: 'pending', failure: '', requestId: '', approvedScene: '', startedAt: null, finishedAt: null, unchanged: false };
    state.rule!.activeProposalId = state.proposal.id;
    state.rule!.latest = { condition: 'missing', observedAt, targetAvailable: true };
  }
  if (saved) { state.rule = { automationId: automation, versionId: version, digest: hash, revision: 1, collection: 'Fixture collection', input: 'Fixture input', scene: 'Fallback <img src=x onerror=alert(1)>', enabled: true, latched: false, activeProposalId: '', updatedAt: new Date().toISOString(), latest: null }; makeProposal(); }
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/account') return route.fulfill({ json: { authenticated: true, deploymentMode: 'selfhost', accountId: account, activeTenantId: state.workspace, csrfToken: 'fixture-csrf', tenants: [{ id: state.workspace, name: 'OBS fixture workspace', role: state.role, plan: 'unlimited', retentionDays: 30 }] } });
    if (!path.includes('/obs-fallback/')) return route.fulfill({ status: 503, body: 'Unrelated fixture service unavailable' });
    const op = path.split('/').at(-1)!; const body = route.request().postDataJSON() as Record<string, unknown>;
    state.calls.push({ op, body });
    expect(route.request().method()).toBe('POST');
    expect(route.request().headers()['x-overlaid-csrf']).toBe('fixture-csrf');
    expect(route.request().headers()['x-overlay-operator-secret']).toBeUndefined();
    if (state.workspace !== tenant) return route.fulfill({ json: { available: true, rule: null } });
    if (op === 'current') return route.fulfill({ json: state.mode === 'bad-current' ? { available: true, rule: {} } : { available: state.available, rule: state.rule } });
    if (op === 'save') {
      state.rule = { automationId: String(body.automationId), versionId: String(body.versionId), digest: hash, revision: (state.rule?.revision || 0) + 1, collection: String(body.collection), input: String(body.input), scene: String(body.scene), enabled: body.enabled === true, latched: false, activeProposalId: '', updatedAt: new Date().toISOString(), latest: null };
      makeProposal(); return route.fulfill({ json: { rule: state.rule } });
    }
    if (op === 'disable') {
      state.rule!.enabled = false; state.rule!.revision++;
      if (state.proposal?.state === 'pending') { state.proposal.state = 'refused'; state.proposal.failure = 'rule_changed'; state.proposal.finishedAt = new Date().toISOString(); }
      return route.fulfill({ json: { rule: state.rule } });
    }
    if (op === 'lookup') return state.mode === 'purged' ? route.fulfill({ status: 404, body: 'PRIVATE PURGED DETAILS' }) : route.fulfill({ json: { receipt: state.proposal } });
    if (op === 'rehearse') return route.fulfill({ json: { rehearsal: { synthetic: true, condition: body.state, reason: body.state === 'disconnected' ? 'connection_lost' : `input_${body.state}`, scene: state.rule!.scene } } });
    if (op === 'replay') return route.fulfill({ json: { rehearsal: { synthetic: false, proposalId: state.proposal!.id, versionId: state.proposal!.versionId, graphDigest: state.proposal!.graphDigest, reason: 'input_missing', scene: state.proposal!.scene }, receipt: state.proposal } });
    if (op === 'dismiss') { state.proposal!.state = 'dismissed'; state.proposal!.failure = 'dismissed'; state.proposal!.finishedAt = new Date().toISOString(); state.rule!.latched = true; return route.fulfill({ json: { receipt: state.proposal } }); }
    if (op === 'approve') {
      expect(await page.evaluate(() => Object.entries(sessionStorage).some(([k, raw]) => k.startsWith('overlaid.obs-approval.v1.') && JSON.parse(raw).requestId))).toBe(true);
      if (state.mode === 'blocked') await blocked;
      if (!state.rule!.enabled || Date.parse(state.proposal!.deadline) <= Date.now()) return route.fulfill({ status: 409, body: 'PRIVATE GUARD DETAIL' });
      state.effects++;
      state.proposal = { ...state.proposal!, requestId: String(body.requestId), approvedScene: String(body.scene), state: 'succeeded', startedAt: new Date().toISOString(), finishedAt: new Date().toISOString() }; state.rule!.latched = true;
      if (state.mode === 'lost') return route.abort();
      return route.fulfill({ json: { receipt: state.mode === 'malformed' ? { ...state.proposal, approvedScene: 'Different scene' } : state.proposal } });
    }
    return route.fulfill({ status: 404 });
  });
  await page.route('**/readyz', route => route.fulfill({ json: { status: 'ok', checks: [] } }));
  page.on('dialog', dialog => { void dialog.accept(); });
  return { state, makeProposal };
}
function desk(page: Page) { return page.getByRole('region', { name: 'Missing-input fallback', exact: true }); }
async function open(page: Page) { await page.goto('/dashboard/scenes'); await expect(desk(page).getByRole('button', { name: 'Refresh saved state' })).toBeEnabled(); }

test('fallback desk saves a version, explains evidence, replays and approves a frozen override', async ({ page }, info) => {
  const { state } = await fixture(page, false); await open(page);
  await expect(desk(page).getByRole('alert')).toHaveCount(0);
  await desk(page).getByRole('textbox', { name: 'OBS scene collection' }).fill('Fixture collection');
  await desk(page).getByRole('textbox', { name: 'Watched OBS input' }).fill('Fixture input');
  await desk(page).getByRole('textbox', { name: 'Fallback scene', exact: true }).fill('Fallback');
  await desk(page).getByRole('button', { name: 'Save new rule version' }).click();
  await expect(desk(page).getByRole('region', { name: 'Fallback proposal' })).toContainText('Awaiting approval');
  await page.screenshot({ path: info.outputPath('obs-fallback-desktop.png'), fullPage: true });
  await desk(page).getByRole('button', { name: 'Replay original observation' }).click();
  await expect(desk(page).getByRole('status').filter({ hasText: 'Original observation' })).toContainText('proposed scene: Fallback');
  await desk(page).getByRole('textbox', { name: 'Override scene (optional)' }).fill('Override');
  await desk(page).getByRole('button', { name: 'Approve scene change' }).click();
  await expect(desk(page)).toContainText('OBS acknowledged the scene change');
  await expect(desk(page).getByRole('button', { name: 'Check approval outcome' })).toBeEnabled();
  expect(state.effects).toBe(1); expect(state.calls.filter(x => x.op === 'approve')).toHaveLength(1);
  expect(state.calls.find(x => x.op === 'approve')!.body.scene).toBe('Override');
});

for (const mode of ['lost', 'malformed']) test(`fallback ${mode} receipt survives reload with only original lookup`, async ({ page }) => {
  const { state } = await fixture(page); state.mode = mode; await open(page);
  await desk(page).getByRole('textbox', { name: 'Override scene (optional)' }).fill('Override');
  await desk(page).getByRole('button', { name: 'Approve scene change' }).click();
  await expect(desk(page).getByRole('button', { name: 'Check approval outcome' })).toBeEnabled();
  const original = state.calls.find(x => x.op === 'approve')!;
  await expect(desk(page).getByRole('alert')).not.toContainText('PRIVATE');
  await page.reload();
  await expect(desk(page).getByRole('button', { name: 'Check approval outcome' })).toBeEnabled();
  await desk(page).getByRole('button', { name: 'Check approval outcome' }).click();
  await expect(desk(page)).toContainText('OBS acknowledged the scene change');
  await expect(desk(page).getByRole('button', { name: 'Approve scene change' })).toHaveCount(0);
  expect(state.calls.filter(x => x.op === 'approve')).toEqual([original]); expect(state.effects).toBe(1);
});

test('fallback missing outcome stays unresolved and independent disable works during approval', async ({ page }) => {
  const { state } = await fixture(page); state.mode = 'blocked'; await open(page);
  await desk(page).getByRole('button', { name: 'Approve scene change' }).click();
  await expect.poll(() => state.calls.filter(x => x.op === 'approve').length).toBe(1);
  await expect(desk(page).getByRole('button', { name: 'Disable rule' })).toBeEnabled();
  await desk(page).getByRole('button', { name: 'Disable rule' }).click();
  await expect(desk(page)).toContainText('Rule disabled.');
  state.release();
  await expect(desk(page).getByRole('button', { name: 'Check approval outcome' })).toBeEnabled();
  state.mode = 'purged';
  await desk(page).getByRole('button', { name: 'Check approval outcome' }).click();
  await expect(desk(page).getByRole('alert')).not.toContainText('PRIVATE');
  await expect(desk(page).getByRole('button', { name: 'Check approval outcome' })).toBeEnabled();
  expect(state.effects).toBe(0); expect(state.calls.filter(x => x.op === 'approve')).toHaveLength(1);
});

test('fallback narrow layout, escaped names, rehearsal, dismissal and stale evidence', async ({ page }, info) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const { state, makeProposal } = await fixture(page); await open(page);
  await expect(desk(page).locator('img')).toHaveCount(0);
  await desk(page).getByRole('combobox', { name: 'Synthetic input condition' }).selectOption('muted');
  await desk(page).getByRole('button', { name: 'Rehearse without changing OBS' }).click();
  await expect(desk(page).getByRole('status')).toContainText('intentionally muted');
  await desk(page).getByRole('button', { name: 'Dismiss proposal' }).click();
  await expect(desk(page)).toContainText('Proposal dismissed');
  expect(state.effects).toBe(0);
  makeProposal(-20000);
  await desk(page).getByRole('button', { name: 'Refresh saved state' }).click();
  await expect(desk(page).getByRole('button', { name: 'Approve scene change' })).toBeDisabled();
  await expect(desk(page)).toContainText('This original proposal is stale.');
  await page.screenshot({ path: info.outputPath('obs-fallback-narrow.png'), fullPage: true });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test('fallback scope change, permission and unreadable storage fence old approval state', async ({ page }) => {
  const { state } = await fixture(page); state.mode = 'lost'; await open(page);
  await desk(page).getByRole('button', { name: 'Approve scene change' }).click();
  await expect(desk(page).getByRole('button', { name: 'Check approval outcome' })).toBeEnabled();
  state.workspace = other; await page.reload();
  await expect(desk(page)).toContainText('No fallback rule is saved');
  await expect(desk(page).getByRole('button', { name: 'Check approval outcome' })).toHaveCount(0);
  expect(state.calls.filter(x => x.op === 'approve')).toHaveLength(1);
  state.role = 'moderator'; await page.reload();
  await expect(desk(page)).toContainText('Configure permission is required');
  state.role = 'owner'; state.workspace = tenant;
  await page.evaluate(() => { for (const key of Object.keys(sessionStorage)) if (key.startsWith('overlaid.obs-approval.v1.')) sessionStorage.setItem(key, '{unreadable'); });
  await page.reload();
  await expect(desk(page)).toContainText('Saved approval is unreadable');
  await expect(desk(page).getByRole('button', { name: 'Approve scene change' })).toHaveCount(0);
  expect(state.calls.filter(x => x.op === 'approve')).toHaveLength(1);
});
