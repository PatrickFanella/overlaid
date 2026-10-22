import { expect, it } from 'vitest';
import type { VersionScope } from './automationVersions';
import { operatorIdentities, type RehearsalIntent } from './operatorRehearsal';
import { decodeTraceCatalogue } from './operatorTraceHistory';
const scope: VersionScope = { accountId: '11111111-1111-4111-8111-111111111111', tenantId: '22222222-2222-4222-8222-222222222222', csrf: 'private', baseURL: 'http://example.test' };
const source: RehearsalIntent = { format: 1, phase: 'submitted', requestId: '66666666-6666-4666-8666-666666666666', automationId: '33333333-3333-4333-8333-333333333333', versionId: '44444444-4444-4444-8444-444444444444', documentDigest: 'a'.repeat(64), graphDigest: 'a'.repeat(64) };
async function fixture() {
  const ids = await operatorIdentities(scope, source);
  return { run: { ...ids, automationId: source.automationId, versionId: source.versionId, graphDigest: source.graphDigest, createdAt: '2026-10-21T23:00:00.123456Z', deadline: '2026-10-21T23:02:00.123456Z', unchanged: true, state: 'admitted_metadata' }, traces: [{ traceId: 'a'.repeat(64), versionId: source.versionId, mode: 'reproduce', digest: 'b'.repeat(64), createdAt: '2026-10-21T23:01:00.123457Z' }, { traceId: 'c'.repeat(64), versionId: source.versionId, mode: 'current_config', digest: 'b'.repeat(64), createdAt: '2026-10-21T18:01:00.123456-05:00' }] };
}
it('keeps discovery source-bound and preserves sub-millisecond save ordering', async () => {
  const raw = await fixture();
  expect((await decodeTraceCatalogue(raw, scope, source)).traces).toHaveLength(2);
  expect((await decodeTraceCatalogue({ ...raw, traces: [] }, scope, source)).traces).toEqual([]);
  await expect(decodeTraceCatalogue({ ...raw, traces: [...raw.traces].reverse() }, scope, source)).rejects.toThrow();
  await expect(decodeTraceCatalogue(raw, scope, { ...source, phase: 'prepared' })).rejects.toThrow();
  await expect(decodeTraceCatalogue(raw, { ...scope, accountId: scope.tenantId }, source)).rejects.toThrow();
});
it('rejects crossed, duplicated, disordered and over-cap selections without partial output', async () => {
  const raw = await fixture();
  const first = raw.traces[0]; const second = raw.traces[1];
  for (const traces of [[{ ...first, traceId: 'PRIVATE' }], [{ ...first, versionId: scope.accountId }], [{ ...first, mode: 'execute' }], [{ ...first, digest: '' }], [{ ...first, createdAt: '2026-02-30T00:00:00Z' }], [first, first], [first, { ...second, mode: 'reproduce' }], Array(9).fill(first), [first, { ...second, createdAt: first.createdAt }]]) await expect(decodeTraceCatalogue({ ...raw, traces }, scope, source)).rejects.toThrow();
  const exact = Array.from({ length: 8 }, (_, i) => ({ ...first, traceId: (15 - i).toString(16).repeat(64), mode: 'current_config', versionId: `44444444-4444-4444-8444-${String(i).padStart(12, '0')}` }));
  expect((await decodeTraceCatalogue({ ...raw, traces: exact }, scope, source)).traces).toHaveLength(8);
  const safe = await decodeTraceCatalogue({ ...raw, traces: [{ ...first, privateBody: 'PRIVATE' }] }, scope, source);
  expect(JSON.stringify(safe)).not.toContain('PRIVATE');
});
