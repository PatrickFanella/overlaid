import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { decodeOperatorRehearsal, decodeOperatorRun, decodeRehearsalIntent, operatorIdentities, rehearsalStorageKey, type RehearsalIntent } from './operatorRehearsal';
import type { VersionScope } from './automationVersions';
import { RehearsalReport } from '../routes/dashboard/OperatorRehearsalDesk';

const scope: VersionScope = { accountId: '11111111-1111-4111-8111-111111111111', tenantId: '22222222-2222-4222-8222-222222222222', csrf: 'private-csrf', baseURL: 'http://example.test' };
const intent: RehearsalIntent = { format: 1, phase: 'prepared', requestId: '66666666-6666-4666-8666-666666666666', automationId: '33333333-3333-4333-8333-333333333333', versionId: '44444444-4444-4444-8444-444444444444', documentDigest: 'a'.repeat(64), graphDigest: 'a'.repeat(64) };
async function fixture() {
  const ids = await operatorIdentities(scope, intent);
  const rawRun = { ...ids, automationId: intent.automationId, versionId: intent.versionId, graphDigest: intent.graphDigest, createdAt: '2026-10-21T23:00:00.123456Z', deadline: '2026-10-21T23:02:00.123456Z', unchanged: true, state: 'admitted_metadata', privateExtra: 'PRIVATE SECRET' };
  const run = await decodeOperatorRun(rawRun, scope, intent);
  const captureDigest = 'b'.repeat(64);
  const rawIdentity = JSON.stringify(['overlaid.engine.rehearsal.v1', intent.graphDigest, intent.graphDigest, run.occurrenceId, 'reproduce', captureDigest, '100']);
  const bytes = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(rawIdentity));
  const id = [...new Uint8Array(bytes)].map(b => b.toString(16).padStart(2, '0')).join('');
  const admission = { sequence: 1, parent: 0, automationId: intent.automationId, versionId: intent.versionId, nodeId: 'button', kind: 'admission', outcome: 'checked', reason: 'matched' };
  const action = { ...admission, sequence: 2, nodeId: 'root', kind: 'action', outcome: 'proposed', reason: 'opaque_arguments' };
  const raw = { captureVersion: 1, selectedVersionId: intent.versionId, run: rawRun, rehearsal: { id, mode: 'reproduce', originalGraphDigest: intent.graphDigest, graphDigest: intent.graphDigest, captureDigest, clock: rawRun.createdAt, seed: 0, status: 'proposed', reason: 'supported_flow', steps: [admission, action], droppedSteps: 0, proposals: [{ sequence: 2, automationId: intent.automationId, versionId: intent.versionId, nodeId: 'root', declaredCapability: 'chat.send', opaqueArguments: [{ name: 'text', value: '<img src=x onerror=alert(1)> $(user)' }] }] } };
  return { rawRun, run, raw };
}

describe('operator request identity and storage', () => {
  it('uses fixed server-v1 JSON-array identity domains and binds account/workspace/request/version/digests', async () => {
    const first = await operatorIdentities(scope, intent);
    expect(first).toEqual({ occurrenceId: 'd273c531abcb467861e72918c8838ad22a98dbf7c098f99fb67923a5aff0320f', runId: '3833bb6989c2f151366e627856c4609f15ce5b8259e8ab21eae390682374c269' });
    for (const other of [{ ...intent, requestId: scope.accountId }, { ...intent, versionId: scope.accountId }, { ...intent, graphDigest: 'c'.repeat(64) }]) expect(await operatorIdentities(scope, other)).not.toEqual(first);
    expect(await operatorIdentities({ ...scope, accountId: scope.tenantId }, intent)).not.toEqual(first);
  });
  it('retains only the original bounded intent and refuses corrupt stages or fields without inventing IDs', () => {
    expect(decodeRehearsalIntent(JSON.stringify(intent))).toEqual(intent);
    expect(decodeRehearsalIntent(JSON.stringify({ ...intent, phase: 'submitted' }))?.requestId).toBe(intent.requestId);
    expect(decodeRehearsalIntent(null)).toBeNull();
    for (const raw of ['{', '{}', JSON.stringify({ ...intent, phase: 'delivered' }), JSON.stringify({ ...intent, requestId: '' }), JSON.stringify({ ...intent, graphDigest: 'PRIVATE' })]) expect(() => decodeRehearsalIntent(raw)).toThrow('unreadable');
    expect(rehearsalStorageKey(scope)).not.toBe(rehearsalStorageKey({ ...scope, tenantId: scope.accountId }));
    expect(rehearsalStorageKey(scope)).not.toContain(scope.csrf);
  });
});
describe('operator admission/rehearsal receipt projection', () => {
  it('checks original identity and strips non-allowlisted receipt fields', async () => {
    const { rawRun, run } = await fixture();
    expect(JSON.stringify(run)).not.toContain('PRIVATE');
    for (const change of [{ state: 'delivered' }, { automationId: scope.accountId }, { versionId: scope.accountId }, { graphDigest: 'b'.repeat(64) }, { occurrenceId: 'b'.repeat(64) }, { runId: 'b'.repeat(64) }, { createdAt: '2026-02-30T00:00:00Z' }, { deadline: rawRun.createdAt }, { unchanged: 'true' }]) await expect(decodeOperatorRun({ ...rawRun, ...change }, scope, intent)).rejects.toThrow();
    await expect(decodeOperatorRun(rawRun, { ...scope, accountId: scope.tenantId }, intent)).rejects.toThrow();
  });
  it('renders only a bounded private report with opaque, escaped proposals and no execution control', async () => {
    const { run, raw } = await fixture();
    const report = await decodeOperatorRehearsal(raw, scope, intent, run, 'reproduce');
    const html = renderToStaticMarkup(<RehearsalReport report={report} />);
    expect(html).toContain('&lt;img'); expect(html).toContain('$(user)'); expect(html).not.toContain('<img');
    expect(html).toContain('not execution permission'); expect(html).not.toContain('<button'); expect(JSON.stringify(report)).not.toContain('PRIVATE');
    const noArgs = { ...raw, rehearsal: { ...raw.rehearsal, proposals: [{ ...raw.rehearsal.proposals[0], opaqueArguments: null }] } };
    await expect(decodeOperatorRehearsal(noArgs, scope, intent, run, 'reproduce')).resolves.toMatchObject({ proposals: [{ opaqueArguments: [] }] });
  });
  it('rejects crossed capture/report/step/proposal identity and unsupported capture formats', async () => {
    const { run, raw } = await fixture();
    for (const change of [{ seed: 1 }, { mode: 'execute' }, { status: 'delivered' }, { reason: 'delivered' }, { steps: [{ ...raw.rehearsal.steps[0], reason: 'private_provider_detail' }] }, { steps: raw.rehearsal.steps, proposals: [] }, { id: 'f'.repeat(64) }, { originalGraphDigest: 'b'.repeat(64) }, { clock: '2026-10-21T23:00:00.123457Z' }, { droppedSteps: -1 }, { steps: [] }, { steps: [raw.rehearsal.steps[1]] }, { proposals: [raw.rehearsal.proposals[0], raw.rehearsal.proposals[0]] }, { proposals: [{ ...raw.rehearsal.proposals[0], sequence: 1 }] }, { proposals: [{ ...raw.rehearsal.proposals[0], opaqueArguments: [{ name: 'text', value: 'x'.repeat(65536) }] }] }]) await expect(decodeOperatorRehearsal({ ...raw, rehearsal: { ...raw.rehearsal, ...change } }, scope, intent, run, 'reproduce')).rejects.toThrow();
    await expect(decodeOperatorRehearsal({ ...raw, captureVersion: 2 }, scope, intent, run, 'reproduce')).rejects.toThrow();
    await expect(decodeOperatorRehearsal({ ...raw, selectedVersionId: scope.accountId }, scope, intent, run, 'reproduce')).rejects.toThrow();
    await expect(decodeOperatorRehearsal(raw, scope, intent, run, 'current_config')).rejects.toThrow();
  });
  it('allows equivalent offset clocks without discarding sub-millisecond source precision', async () => {
    const { run, raw } = await fixture();
    raw.rehearsal.clock = '2026-10-21T18:00:00.123456-05:00';
    await expect(decodeOperatorRehearsal(raw, scope, intent, run, 'reproduce')).resolves.toMatchObject({ clock: raw.rehearsal.clock });
    raw.rehearsal.clock = '2026-10-21T18:00:00.123457-05:00';
    await expect(decodeOperatorRehearsal(raw, scope, intent, run, 'reproduce')).rejects.toThrow();
  });
});
