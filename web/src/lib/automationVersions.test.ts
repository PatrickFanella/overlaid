import { afterEach, describe, expect, it, vi } from 'vitest';
import { decodeAutomations, decodeGraph, decodeVersionDesk, decodeVersions, draftDocument, verifyPublication, verifyValidation, versionRequest, versionStorageKey, type Draft, type VersionScope } from './automationVersions';

const account = '11111111-1111-4111-8111-111111111111';
const tenant = '22222222-2222-4222-8222-222222222222';
const automation = '33333333-3333-4333-8333-333333333333';
const version = '44444444-4444-4444-8444-444444444444';
const hash = 'a'.repeat(64);
const at = '2026-10-21T23:00:00Z';
const scope: VersionScope = { accountId: account, tenantId: tenant, csrf: 'snapshot-csrf', baseURL: 'http://example.test' };
function draft(): Draft { return { automationId: automation, versionId: version, expected: '', text: JSON.stringify({ tenantId: tenant, automationId: automation, versionId: version, nodes: [] }) }; }
const publication = { automationId: automation, versionId: version, digest: hash, graphDigest: hash, createdAt: at, unchanged: false };
afterEach(() => vi.unstubAllGlobals());

describe('automation version receipts and scope', () => {
  it('projects metadata only and rejects contradictory list and graph identities', () => {
    expect(decodeAutomations({ automations: [{ automationId: automation, currentVersionId: version, digest: hash, versionCount: 1, updatedAt: at, versionCreatedAt: at, private: 'PRIVATE' }] })).not.toEqual(expect.arrayContaining([expect.objectContaining({ private: 'PRIVATE' })]));
    expect(() => decodeVersions({ automationId: tenant, currentVersionId: version, versions: [] }, automation)).toThrow();
    expect(() => decodeVersions({ automationId: automation, currentVersionId: account, versions: [{ versionId: version, digest: hash, createdAt: at }] }, automation)).toThrow();
    expect(() => decodeGraph({ automationId: automation, versionId: version, graphDigest: hash, versions: [{ tenantId: account, automationId: automation, versionId: version }] }, tenant, automation, version)).toThrow();
  });
  it('refuses malformed or contradictory publication/validation receipts', () => {
    expect(() => verifyPublication(publication, automation, version, hash)).not.toThrow();
    for (const change of [{ automationId: tenant }, { versionId: account }, { digest: 'b'.repeat(64) }, { graphDigest: 'bad' }, { createdAt: 'now' }, { createdAt: '2026-02-30T00:00:00Z' }, { createdAt: '2026-02-01T24:00:00Z' }, { unchanged: 'true' }]) expect(() => verifyPublication({ ...publication, ...change }, automation, version, hash)).toThrow();
    expect(verifyValidation({ validation: 'structure_only', automationId: automation, versionId: version, digest: hash }, draft())).toBe(hash);
    expect(() => verifyValidation({ ...publication, validation: 'executed' }, draft())).toThrow();
  });
  it('sends only snapshot session credentials and never mutable global tenant/operator authority', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ automations: [] }) });
    vi.stubGlobal('fetch', fetchMock);
    const controller = new AbortController();
    await versionRequest(scope, 'list', {}, controller.signal);
    expect(fetchMock).toHaveBeenCalledWith(`http://example.test/api/tenants/${tenant}/automations/list`, { method: 'POST', credentials: 'include', headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-Overlaid-CSRF': 'snapshot-csrf' }, body: '{}', signal: controller.signal });
  });
  it('does not read private failure text or surface malformed JSON details', async () => {
    const text = vi.fn();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 403, text }));
    await expect(versionRequest(scope, 'publish', {}, new AbortController().signal)).rejects.toThrow('(403)');
    expect(text).not.toHaveBeenCalled();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => { throw new Error('PRIVATE RAW RESPONSE'); } }));
    await expect(versionRequest(scope, 'list', {}, new AbortController().signal)).rejects.not.toThrow('PRIVATE');
  });
  it('fences late responses after abort even when transport ignores cancellation', async () => {
    const controller = new AbortController();
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => { controller.abort(); return { ok: true, json: async () => publication }; }));
    await expect(versionRequest(scope, 'publish', {}, controller.signal)).rejects.toThrow('canceled');
  });
});
describe('scoped tab version intents', () => {
  it('retains exact draft text and frozen publication identity across reload', () => {
    const d = draft(); const raw = JSON.stringify({ format: 1, draft: d, pending: { operation: 'publish', draft: d, digest: hash } });
    const restored = decodeVersionDesk(raw, tenant);
    expect(restored.draft?.text).toBe(d.text);
    expect(restored.pending).toEqual({ operation: 'publish', draft: d, digest: hash });
  });
  it('rejects corrupt, mismatched or foreign frozen intents without clearing them', () => {
    for (const raw of ['{', '{}', JSON.stringify({ format: 1, draft: draft(), pending: { operation: 'publish', draft: { ...draft(), versionId: account }, digest: hash } }), JSON.stringify({ format: 1, draft: draft(), pending: { operation: 'execute' } })]) expect(() => decodeVersionDesk(raw, tenant)).toThrow('unreadable');
    const d = draft();
    expect(() => decodeVersionDesk(JSON.stringify({ format: 1, draft: d, pending: { operation: 'publish', draft: d, digest: hash } }), account)).toThrow();
  });
  it('retains invalid JSON while editing, but rejects changed identity or excessive bytes before validation/publication', () => {
    expect(decodeVersionDesk(JSON.stringify({ format: 1, draft: { ...draft(), text: '{' }, pending: null }), tenant).draft?.text).toBe('{');
    expect(() => draftDocument({ ...draft(), text: '{' }, tenant)).toThrow('valid JSON');
    expect(() => draftDocument(draft(), account)).toThrow('IDs unchanged');
    expect(() => draftDocument({ ...draft(), text: '🫠'.repeat(16000) }, tenant)).toThrow('byte');
  });
  it('separates API account and workspace storage without storing CSRF credentials', () => {
    const key = versionStorageKey(scope);
    for (const other of [{ ...scope, accountId: tenant }, { ...scope, tenantId: account }, { ...scope, baseURL: 'http://other.test' }]) expect(versionStorageKey(other)).not.toBe(key);
    expect(key).not.toContain(scope.csrf);
    expect(versionStorageKey({ ...scope, csrf: 'new-session' })).toBe(key);
  });
});
