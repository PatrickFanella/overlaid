import { decodeHistoryTrace, decodeTraceCatalogue } from './operatorTraceHistory';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { decodeSavedOperatorTrace, decodeTraceReference, traceStorageKey, type TraceReference } from './operatorTraces';
import { operatorIdentities, operatorIdentity, type RehearsalIntent, type OperatorRun } from './operatorRehearsal';
import type { VersionScope } from './automationVersions';
import { SavedTraceSummary } from '../routes/dashboard/SavedTraceSummary';
const scope: VersionScope = { accountId: '11111111-1111-4111-8111-111111111111', tenantId: '22222222-2222-4222-8222-222222222222', csrf: 'private', baseURL: 'http://example.test' };
const source: RehearsalIntent = { format: 1, phase: 'submitted', requestId: '66666666-6666-4666-8666-666666666666', automationId: '33333333-3333-4333-8333-333333333333', versionId: '44444444-4444-4444-8444-444444444444', documentDigest: 'a'.repeat(64), graphDigest: 'a'.repeat(64) };
async function fixture() {
  const ids = await operatorIdentities(scope, source);
  const run: OperatorRun = { ...ids, automationId: source.automationId, versionId: source.versionId, graphDigest: source.graphDigest, createdAt: '2026-10-21T23:00:00.123456Z', deadline: '2026-10-21T23:02:00.123456Z', unchanged: true, state: 'admitted_metadata' };
  const captureDigest = 'b'.repeat(64);
  const traceId = await operatorIdentity(['overlaid.engine.rehearsal.v1', source.graphDigest, source.graphDigest, ids.occurrenceId, 'reproduce', captureDigest, '100']);
  const ref: TraceReference = { format: 1, source, traceId, mode: 'reproduce', versionId: source.versionId, graphDigest: source.graphDigest, captureDigest };
  const document = { formatVersion: 1, tenantId: scope.tenantId, id: traceId, runId: run.runId, automationId: source.automationId, versionId: source.versionId, mode: 'reproduce', captureVersion: 1, originalGraphDigest: source.graphDigest, graphDigest: source.graphDigest, captureDigest, clock: run.createdAt, seed: 0, status: 'proposed', reason: 'supported_flow', steps: [{ Sequence: 1, Parent: 0, AutomationID: source.automationId, VersionID: source.versionId, NodeID: 'button', Kind: 'admission', Outcome: 'checked', Reason: 'matched' }, { Sequence: 2, Parent: 0, AutomationID: source.automationId, VersionID: source.versionId, NodeID: 'root', Kind: 'action', Outcome: 'proposed', Reason: 'opaque_arguments' }], droppedSteps: 0 };
  const raw = { document, digest: await digest(document), createdAt: run.createdAt, unchanged: false };
  return { run, ref, raw };
}
async function digest(d: unknown) { const bytes = await crypto.subtle.digest('SHA-256', new TextEncoder().encode('overlaid.engine.operator-trace.v1\0' + JSON.stringify(d))); return [...new Uint8Array(bytes)].map(b => b.toString(16).padStart(2, '0')).join(''); }
describe('saved metadata trace contract', () => {
  it('validates canonical metadata and renders no proposals or effect controls', async () => {
    const { ref, run, raw } = await fixture();
    const result = await decodeSavedOperatorTrace(raw, scope, ref, run);
    expect(result.digest).toBe(raw.digest);
    const html = renderToStaticMarkup(<SavedTraceSummary trace={result} />);
    expect(html).toContain('stored snapshot'); expect(html).not.toContain('<button');
    expect(result).not.toHaveProperty('proposals'); expect(result).not.toHaveProperty('opaqueArguments');
  });
  it('refuses altered canonical bytes, extra private fields and mismatched original binding even with a new digest', async () => {
    const { ref, run, raw } = await fixture();
    for (const change of [{ seed: 1 }, { formatVersion: 2 }, { captureVersion: 2 }, { runId: 'c'.repeat(64) }, { tenantId: scope.accountId }, { versionId: scope.accountId }, { graphDigest: 'c'.repeat(64) }, { clock: '2026-10-21T23:00:00.123457Z' }, { proposals: ['PRIVATE'] }, { steps: [{ ...raw.document.steps[0], PRIVATE: 'secret' }] }, { steps: [{ ...raw.document.steps[0], Reason: 'private_error' }] }, { droppedSteps: 1 }]) {
      const document = { ...raw.document, ...change };
      await expect(decodeSavedOperatorTrace({ ...raw, document, digest: await digest(document) }, scope, ref, run)).rejects.toThrow();
    }
    const { id, ...rest } = raw.document; const reordered = { id, ...rest };
    await expect(decodeSavedOperatorTrace({ ...raw, document: reordered, digest: await digest(reordered) }, scope, ref, run)).rejects.toThrow();
    await expect(decodeSavedOperatorTrace({ ...raw, digest: 'f'.repeat(64) }, scope, ref, run)).rejects.toThrow();
    await expect(decodeSavedOperatorTrace(raw, { ...scope, accountId: scope.tenantId }, ref, run)).rejects.toThrow();
    await expect(decodeSavedOperatorTrace(raw, scope, { ...ref, traceId: 'f'.repeat(64) }, run)).rejects.toThrow();
  });
  it('preserves the fixed original handle and rejects corruption instead of creating an identity', async () => {
    const { ref } = await fixture();
    expect(decodeTraceReference(JSON.stringify(ref))).toEqual(ref);
    expect(decodeTraceReference(null)).toBeNull();
    for (const raw of ['{', '{}', 'x'.repeat(4097), JSON.stringify({ ...ref, source: { ...source, phase: 'prepared' } }), JSON.stringify({ ...ref, graphDigest: 'b'.repeat(64) }), JSON.stringify({ ...ref, traceId: '' })]) expect(() => decodeTraceReference(raw)).toThrow('unreadable');
    expect(traceStorageKey(scope)).not.toBe(traceStorageKey({ ...scope, tenantId: scope.accountId }));
    expect(traceStorageKey(scope)).not.toContain(scope.csrf);
  });
});

it('binds a loaded history body to its validated summary, source and exact save time', async () => {
  const { ref, run, raw } = await fixture();
  const selected = { traceId: ref.traceId, versionId: ref.versionId, mode: ref.mode, digest: raw.digest, createdAt: raw.createdAt };
  const catalogue = await decodeTraceCatalogue({ run, traces: [selected] }, scope, source);
  await expect(decodeHistoryTrace(raw, scope, source, catalogue, catalogue.traces[0])).resolves.toMatchObject({ id: ref.traceId, digest: raw.digest });
  await expect(decodeHistoryTrace(raw, scope, source, catalogue, { ...catalogue.traces[0] })).rejects.toThrow();
  for (const change of [{ digest: 'f'.repeat(64) }, { createdAt: '2026-10-21T23:00:00.123457Z' }, { mode: 'current_config' }, { versionId: scope.accountId }]) {
    const changed = await decodeTraceCatalogue({ run, traces: [{ ...selected, ...change, mode: change.versionId ? 'current_config' : change.mode || selected.mode }] }, scope, source);
    await expect(decodeHistoryTrace(raw, scope, source, changed, changed.traces[0])).rejects.toThrow();
  }
});
