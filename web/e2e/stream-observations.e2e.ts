import { expect, test, type Page } from '@playwright/test';

const at = '2026-10-21T21:05:00Z';
const snapshot = {
  state: 'observed', channelId: 'fixture-channel', observedAt: at,
  lifecycle: { eventId: 'a'.repeat(64), kind: 'stream_online', streamId: 'fixture-stream', occurredAt: '2026-10-21T21:00:00Z', receivedAt: '2026-10-21T21:00:01Z', startedAt: '2026-10-21T20:59:00Z' },
  metadata: { eventId: 'b'.repeat(64), occurredAt: '2026-10-21T21:02:00Z', receivedAt: '2026-10-21T21:02:01Z', data: { title: 'Saved channel title', language: 'en', category_id: 'category', category_name: 'Just Chatting', content_classification_labels: [] } },
};

async function fixture(page: Page) {
  const writes: string[] = [];
  await page.addInitScript(() => sessionStorage.setItem('overlaid.operatorSecret.session.v1', 'operator-fixture'));
  // Every backend request is contained in this fixture, including existing desk
  // status reads. No provider, OBS, real connection or product write is used.
  await page.route('**/api/**', async route => {
    if (route.request().method() !== 'GET') writes.push(route.request().url());
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/account') return route.fulfill({ status: 404 });
    if (path === '/api/newsroom') return route.fulfill({ json: { version: 1, revision: 1, items: [], order: [], shelves: [], history: [], sessions: [], segments: [], questions: [], raffle: { entries: [] }, settings: {}, broadcasts: [], quickLinks: [], bundles: [], automation: { enabled: false, seen: [] } } });
    if (path === '/api/newsroom/broadcasts') return route.fulfill({ json: { broadcasts: [], total: 0, nextCursor: '' } });
    if (path === '/api/newsroom/automation') return route.fulfill({ json: { enabled: false, seen: [] } });
    return route.fulfill({ status: 503, body: 'PRIVATE BACKEND FIXTURE' });
  });
  await page.route('**/readyz', route => route.fulfill({ json: { status: 'ok', checks: [] } }));
  return writes;
}
async function openShows(page: Page) {
  await page.goto('/dashboard/newsroom');
  await page.getByRole('button', { name: 'Shows and recordings', exact: true }).click();
  return page.getByRole('region', { name: 'Saved Twitch observations', exact: true });
}

test('saved stream panel reads historical metadata, clears failed refresh and recovers', async ({ page }) => {
  const writes = await fixture(page);
  let mode = 'observed';
  await page.route('**/api/twitch/stream-observations', route => {
    expect(route.request().method()).toBe('GET');
    expect(route.request().headers()['x-overlay-operator-secret']).toBe('operator-fixture');
    if (mode === 'error') return route.fulfill({ status: 403, body: 'PRIVATE PROVIDER FAILURE' });
    if (mode === 'malformed') return route.fulfill({ json: { ...snapshot, state: 'empty' } });
    if (mode === 'disconnected') return route.fulfill({ json: { state: 'disconnected', observedAt: at, lifecycle: null, metadata: null } });
    if (mode === 'empty') return route.fulfill({ json: { state: 'empty', channelId: 'fixture-channel', observedAt: at, lifecycle: null, metadata: null } });
    return route.fulfill({ json: { ...snapshot, private_native_field: 'PRIVATE INPUT' } });
  });
  const panel = await openShows(page);
  await expect(panel.getByText('Saved channel title')).toBeVisible();
  await expect(panel.getByText('Online event', { exact: true })).toBeVisible();
  await expect(panel.locator('time[datetime="2026-10-21T21:00:01Z"]')).toBeVisible();
  await expect(panel).toContainText('Twitch may have changed');
  await expect(panel).not.toContainText('PRIVATE');
  for (const next of ['error', 'malformed', 'disconnected', 'empty', 'observed']) {
    mode = next;
    await panel.getByRole('button', { name: 'Refresh observations' }).click();
    if (next === 'error' || next === 'malformed') {
      await expect(panel.getByRole('status')).toContainText('Saved Twitch observations are unavailable');
      await expect(panel).not.toContainText('Saved channel title');
      await expect(panel).not.toContainText('PRIVATE');
    } else if (next === 'disconnected') await expect(panel).toContainText('No active Twitch connection');
    else if (next === 'empty') await expect(panel).toContainText('No Twitch lifecycle or channel details');
    else await expect(panel.getByText('Saved channel title')).toBeVisible();
  }
  expect(writes).toEqual([]);
});

test('obsolete observation reads cannot repaint a reopened panel', async ({ page }) => {
  const writes = await fixture(page);
  let hold = true;
  let entered = 0;
  let release!: () => void;
  const barrier = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/twitch/stream-observations', async route => {
    const obsolete = hold;
    entered++;
    if (obsolete) await barrier;
    await route.fulfill({ json: { ...snapshot, metadata: { ...snapshot.metadata, data: { ...snapshot.metadata.data, title: obsolete ? 'OBSOLETE WORKSPACE TEXT' : 'Fresh saved title' } } } }).catch(() => {});
  });
  const panel = await openShows(page);
  await expect.poll(() => entered).toBeGreaterThan(0);
  await expect(panel.getByRole('status')).toContainText('Loading saved');
  await page.getByRole('button', { name: 'Reaction deck', exact: true }).click();
  hold = false;
  await page.getByRole('button', { name: 'Shows and recordings', exact: true }).click();
  await expect(panel.getByText('Fresh saved title')).toBeVisible();
  release();
  await expect(panel).not.toContainText('OBSOLETE WORKSPACE TEXT');
  expect(writes).toEqual([]);
});

test('observation reads time out with a usable retry and no stale result', async ({ page }) => {
  await fixture(page);
  await page.clock.install();
  let hold = true;
  let release!: () => void;
  const barrier = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/twitch/stream-observations', async route => {
    if (hold) await barrier;
    await route.fulfill({ json: snapshot }).catch(() => {});
  });
  const panel = await openShows(page);
  await expect(panel.getByRole('status')).toContainText('Loading saved');
  await page.clock.fastForward(10_001);
  await expect(panel.getByRole('status')).toContainText('unavailable');
  hold = false; release();
  await panel.getByRole('button', { name: 'Refresh observations' }).click();
  await expect(panel.getByText('Saved channel title')).toBeVisible();
});

test('sample observations are labeled and render without a read at a narrow viewport', async ({ page }) => {
  await fixture(page);
  let reads = 0;
  await page.route('**/api/twitch/stream-observations', route => { reads++; return route.fulfill({ json: snapshot }); });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/dashboard/newsroom?demo=1');
  await page.getByRole('button', { name: 'Shows and recordings', exact: true }).click();
  const panel = page.getByRole('region', { name: 'Saved Twitch observations', exact: true });
  await expect(panel.getByText('Sample observations')).toBeVisible();
  await expect(panel).toContainText('Evening desk');
  await expect(panel.getByRole('button', { name: 'Refresh observations' })).toHaveCount(0);
  expect(reads).toBe(0);
  expect(await panel.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true);
});


test('hosted workspace switching discards old reads and uses the new tenant without operator credentials', async ({ page }) => {
  await fixture(page);
  let active = 'tenant-alpha';
  const tenants = [
    { id: 'tenant-alpha', name: 'Workspace Alpha', slug: 'alpha', role: 'editor', plan: 'unlimited', retentionDays: 30 },
    { id: 'tenant-beta', name: 'Workspace Beta', slug: 'beta', role: 'editor', plan: 'unlimited', retentionDays: 30 },
  ];
  await page.route('**/api/account', route => route.fulfill({ json: { authenticated: true, deploymentMode: 'hosted', accountId: 'fixture-account', activeTenantId: active, csrfToken: 'fixture-csrf', tenants } }));
  await page.route('**/api/account/select-tenant', route => {
    expect(route.request().method()).toBe('POST');
    expect(route.request().postDataJSON()).toEqual({ tenantId: 'tenant-beta' });
    active = 'tenant-beta'; return route.fulfill({ json: {} });
  });
  let alphaReads = 0;
  let release!: () => void;
  const barrier = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/twitch/stream-observations', async route => {
    const tenant = route.request().headers()['x-overlaid-tenant'];
    expect(route.request().method()).toBe('GET');
    expect(route.request().headers()['x-overlay-operator-secret']).toBeUndefined();
    expect(route.request().headers()['x-overlaid-csrf']).toBe('fixture-csrf');
    if (tenant === 'tenant-alpha') { alphaReads++; await barrier; }
    else expect(tenant).toBe('tenant-beta');
    await route.fulfill({ json: { ...snapshot, metadata: { ...snapshot.metadata, data: { ...snapshot.metadata.data, title: tenant === 'tenant-alpha' ? 'PRIVATE ALPHA TITLE' : 'Saved Beta channel details' } } } }).catch(() => {});
  });
  await openShows(page);
  await expect.poll(() => alphaReads).toBeGreaterThan(0);
  await page.getByRole('link', { name: 'Account and workspace', exact: true }).click();
  await page.getByText('Workspace Beta', { exact: true }).locator('..').getByRole('button', { name: 'Switch workspace' }).click();
  await expect(page).toHaveURL(/\/dashboard$/);
  await page.getByRole('link', { name: /Newsroom/ }).click();
  await page.getByRole('button', { name: 'Shows and recordings', exact: true }).click();
  const panel = page.getByRole('region', { name: 'Saved Twitch observations', exact: true });
  await expect(panel.getByText('Saved Beta channel details')).toBeVisible();
  release();
  await expect(panel).not.toContainText('PRIVATE ALPHA TITLE');
});

test('long untrusted channel text remains text and fits a narrow observation panel', async ({ page }) => {
  const writes = await fixture(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.route('**/api/twitch/stream-observations', route => route.fulfill({ json: { ...snapshot, lifecycle: null, metadata: { ...snapshot.metadata, data: { ...snapshot.metadata.data, title: '<img src=x onerror=alert(1)>' + '長'.repeat(1000), category_name: 'Category'.repeat(120), content_classification_labels: ['label'.repeat(24)] } } } }));
  const panel = await openShows(page);
  await expect(panel).toContainText('<img src=x onerror=alert(1)>');
  await expect(panel.locator('img')).toHaveCount(0);
  await expect(panel).toContainText('No lifecycle event');
  expect(await panel.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true);
  expect(writes).toEqual([]);
});
