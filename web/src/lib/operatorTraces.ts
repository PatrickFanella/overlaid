import { versionID, versionStorageKey, type VersionScope } from './automationVersions';
import { decodeFlowMetadata, decodeRehearsalIntent, operatorIdentities, operatorIdentity, operatorTimestamp, sameOperatorInstant, type OperatorRun, type RehearsalIntent, type RehearsalMode } from './operatorRehearsal';

export type TraceReference = { format: 1; source: RehearsalIntent; traceId: string; mode: RehearsalMode; versionId: string; graphDigest: string; captureDigest: string };
export type SavedOperatorTrace = ReturnType<typeof decodeFlowMetadata> & { id: string; runId: string; versionId: string; mode: RehearsalMode; originalGraphDigest: string; graphDigest: string; captureDigest: string; clock: string; digest: string; createdAt: string; unchanged: boolean };
const fail = (): never => { throw new Error('Invalid saved trace metadata. Keep the original trace intent and check its saved identity.'); };
function record(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) return fail(); return value as Record<string, unknown>; }
function hash(value: unknown): string { return typeof value === 'string' && /^[a-f0-9]{64}$/.test(value) ? value : fail(); }
export function traceStorageKey(scope: VersionScope) { return `${versionStorageKey(scope)}.operator-trace`; }
export function decodeTraceReference(raw: string | null): TraceReference | null {
  if (raw === null) return null;
  try {
    if (raw.length > 4096) return fail();
    const r = record(JSON.parse(raw)); const source = decodeRehearsalIntent(JSON.stringify(r.source));
    if (r.format !== 1 || !source || source.phase !== 'submitted' || !versionID(r.versionId) || !['reproduce', 'current_config'].includes(String(r.mode))) return fail();
    const ref: TraceReference = { format: 1, source, traceId: hash(r.traceId), mode: r.mode as RehearsalMode, versionId: r.versionId, graphDigest: hash(r.graphDigest), captureDigest: hash(r.captureDigest) };
    if (ref.mode === 'reproduce' && (ref.versionId !== source.versionId || ref.graphDigest !== source.graphDigest)) return fail();
    return ref;
  } catch { throw new Error('Saved trace intent is unreadable. Explicitly acknowledge local state before another trace save.'); }
}

export async function decodeSavedOperatorTrace(value: unknown, scope: VersionScope, ref: TraceReference, run: OperatorRun): Promise<SavedOperatorTrace> {
  const top = record(value); const d = record(top.document);
  if (d.formatVersion !== 1 || d.captureVersion !== 1 || d.seed !== 0 || d.tenantId !== scope.tenantId || d.id !== ref.traceId || d.automationId !== ref.source.automationId || d.versionId !== ref.versionId || d.mode !== ref.mode || d.originalGraphDigest !== ref.source.graphDigest || d.graphDigest !== ref.graphDigest || d.captureDigest !== ref.captureDigest || typeof top.unchanged !== 'boolean' || !Array.isArray(d.steps)) return fail();
  const ids = await operatorIdentities(scope, ref.source);
  const reportId = await operatorIdentity(['overlaid.engine.rehearsal.v1', ref.graphDigest, ref.source.graphDigest, ids.occurrenceId, ref.mode, ref.captureDigest, '100']);
  const clock = operatorTimestamp(d.clock);
  if (d.runId !== ids.runId || run.runId !== ids.runId || run.occurrenceId !== ids.occurrenceId || run.automationId !== ref.source.automationId || run.versionId !== ref.source.versionId || run.graphDigest !== ref.source.graphDigest || reportId !== ref.traceId || !sameOperatorInstant(clock, run.createdAt)) return fail();
  const metadata = decodeFlowMetadata({ status: d.status, reason: d.reason, droppedSteps: d.droppedSteps, steps: d.steps.map(raw => { const s = record(raw); return { sequence: s.Sequence, parent: s.Parent, automationId: s.AutomationID, versionId: s.VersionID, nodeId: s.NodeID, kind: s.Kind, outcome: s.Outcome, reason: s.Reason }; }) });
  if (metadata.steps[0].kind !== 'admission' || metadata.steps[0].automationId !== ref.source.automationId || metadata.steps[0].versionId !== ref.versionId) return fail();
  // Match the stored format-1 Go struct order and capitalized FlowStep codec.
  // All allowed values are ASCII metadata; Go's HTML escaping changes none.
  const canonical = { formatVersion: 1, tenantId: scope.tenantId, id: ref.traceId, runId: ids.runId, automationId: ref.source.automationId, versionId: ref.versionId, mode: ref.mode, captureVersion: 1, originalGraphDigest: ref.source.graphDigest, graphDigest: ref.graphDigest, captureDigest: ref.captureDigest, clock, seed: 0, status: metadata.status, reason: metadata.reason, steps: metadata.steps.map(s => ({ Sequence: s.sequence, Parent: s.parent, AutomationID: s.automationId, VersionID: s.versionId, NodeID: s.nodeId, Kind: s.kind, Outcome: s.outcome, Reason: s.reason })), droppedSteps: metadata.droppedSteps };
  const raw = JSON.stringify(canonical);
  if (JSON.stringify(d) !== raw || new TextEncoder().encode(raw).length > 65536) return fail();
  const bytes = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(`overlaid.engine.operator-trace.v1\0${raw}`));
  const digest = [...new Uint8Array(bytes)].map(b => b.toString(16).padStart(2, '0')).join('');
  if (hash(top.digest) !== digest) return fail();
  return { ...metadata, id: ref.traceId, runId: ids.runId, versionId: ref.versionId, mode: ref.mode, originalGraphDigest: ref.source.graphDigest, graphDigest: ref.graphDigest, captureDigest: ref.captureDigest, clock, digest, createdAt: operatorTimestamp(top.createdAt), unchanged: top.unchanged };
}
