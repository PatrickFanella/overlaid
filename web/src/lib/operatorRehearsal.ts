import { versionID, versionStorageKey, type VersionScope } from './automationVersions';

export type RehearsalIntent = { format: 1; phase: 'prepared' | 'submitted'; requestId: string; automationId: string; versionId: string; documentDigest: string; graphDigest: string };
export type OperatorRun = { runId: string; occurrenceId: string; automationId: string; versionId: string; graphDigest: string; createdAt: string; deadline: string; unchanged: boolean; state: 'admitted_metadata' };
export type RehearsalMode = 'reproduce' | 'current_config';
export type RehearsalStep = { sequence: number; parent: number; automationId: string; versionId: string; nodeId: string; kind: string; outcome: string; reason: string };
export type OpaqueProposal = { sequence: number; automationId: string; versionId: string; nodeId: string; declaredCapability: string; opaqueArguments: { name: string; value: string }[] };
export type OperatorRehearsal = { id: string; mode: RehearsalMode; selectedVersionId: string; run: OperatorRun; graphDigest: string; captureDigest: string; clock: string; status: string; reason: string; steps: RehearsalStep[]; droppedSteps: number; proposals: OpaqueProposal[] };
const fail = (): never => { throw new Error('Invalid retained rehearsal metadata. Keep the original request and check its saved occurrence.'); };
function record(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) return fail(); return value as Record<string, unknown>; }
function id(value: unknown): string { return versionID(value) ? value : fail(); }
function hash(value: unknown): string { return typeof value === 'string' && /^[a-f0-9]{64}$/.test(value) ? value : fail(); }
function code(value: unknown): string { return typeof value === 'string' && /^[a-z][a-z0-9_.:-]{0,63}$/.test(value) ? value : fail(); }
function integer(value: unknown, min = 0, max = 10000): number { return Number.isInteger(value) && Number(value) >= min && Number(value) <= max ? Number(value) : fail(); }
function at(value: unknown): string {
  if (typeof value !== 'string' || value.length > 64 || !/^\d{4}-\d{2}-\d{2}T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d{1,9})?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/.test(value) || !Number.isFinite(Date.parse(value)) || new Date(`${value.slice(0, 10)}T00:00:00Z`).toISOString().slice(0, 10) !== value.slice(0, 10)) return fail();
  return value;
}
function sameInstant(a: string, b: string): boolean { return Date.parse(a) === Date.parse(b) && (a.match(/\.(\d+)/)?.[1] || '').padEnd(9, '0') === (b.match(/\.(\d+)/)?.[1] || '').padEnd(9, '0'); }
const admissionReasons = ['matched', 'policy_missing', 'filters_rejected', 'filters_unavailable', 'group_rejected', 'group_unavailable'];
function validStep(s: RehearsalStep): boolean {
  if (s.kind === 'admission') return s.sequence === 1 && s.outcome === 'checked' && admissionReasons.includes(s.reason);
  if (s.kind === 'condition_group') return s.outcome === 'checked' && ['matched', 'not_matched', 'unavailable'].includes(s.reason);
  if (s.kind === 'action') return s.outcome === 'proposed' && s.reason === 'opaque_arguments';
  if (s.kind === 'stop') return s.outcome === 'stopped' && s.reason === 'explicit_stop';
  if (s.outcome === 'unsupported') return ['random', 'delay', 'set_output', 'all', 'preset'].includes(s.kind) && s.reason === 'unsupported_node';
  return ['sequential', 'all', 'loop', 'condition', 'switch', 'preset'].includes(s.kind) && s.outcome === 'visited' && s.reason === 'supported';
}
export function rehearsalStorageKey(scope: VersionScope) { return `${versionStorageKey(scope)}.operator-occurrence`; }
export function decodeRehearsalIntent(raw: string | null): RehearsalIntent | null {
  if (raw === null) return null;
  try {
    if (raw.length > 2048) return fail();
    const r = record(JSON.parse(raw));
    if (r.format !== 1 || !['prepared', 'submitted'].includes(String(r.phase))) return fail();
    return { format: 1, phase: r.phase as RehearsalIntent['phase'], requestId: id(r.requestId), automationId: id(r.automationId), versionId: id(r.versionId), documentDigest: hash(r.documentDigest), graphDigest: hash(r.graphDigest) };
  } catch { throw new Error('Saved rehearsal intent is unreadable. Explicitly acknowledge local state before preparing another occurrence.'); }
}
// These exact JSON-array codecs mirror the immutable server v1 identity domains.
// Matching metadata remains evidence of admission only, never effect permission.
async function identity(parts: string[]): Promise<string> {
  const bytes = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(JSON.stringify(parts)));
  return [...new Uint8Array(bytes)].map(b => b.toString(16).padStart(2, '0')).join('');
}
export async function operatorIdentities(scope: VersionScope, intent: RehearsalIntent) {
  const occurrenceId = await identity(['overlaid.engine.operator-occurrence.v1', scope.tenantId, scope.accountId, intent.requestId]);
  const root = await identity(['overlaid.engine.run.v1', scope.tenantId, occurrenceId, intent.automationId, intent.versionId, intent.documentDigest, 'execute']);
  return { occurrenceId, runId: await identity(['overlaid.engine.pinned-run.v1', root, intent.graphDigest]) };
}
export async function decodeOperatorRun(value: unknown, scope: VersionScope, intent: RehearsalIntent): Promise<OperatorRun> {
  const r = record(value); const expected = await operatorIdentities(scope, intent);
  if (r.state !== 'admitted_metadata' || r.automationId !== intent.automationId || r.versionId !== intent.versionId || r.graphDigest !== intent.graphDigest || r.occurrenceId !== expected.occurrenceId || r.runId !== expected.runId || typeof r.unchanged !== 'boolean') return fail();
  const createdAt = at(r.createdAt); const deadline = at(r.deadline);
  if (Date.parse(deadline) <= Date.parse(createdAt)) return fail();
  return { ...expected, automationId: intent.automationId, versionId: intent.versionId, graphDigest: intent.graphDigest, createdAt, deadline, unchanged: r.unchanged, state: 'admitted_metadata' };
}
export async function decodeOperatorRehearsal(value: unknown, scope: VersionScope, intent: RehearsalIntent, accepted: OperatorRun, mode: RehearsalMode): Promise<OperatorRehearsal> {
  const top = record(value); const r = record(top.rehearsal);
  const run = await decodeOperatorRun(top.run, scope, intent);
  const clock = at(r.clock);
  if (!sameInstant(run.createdAt, accepted.createdAt) || !sameInstant(run.deadline, accepted.deadline) || top.captureVersion !== 1 || r.seed !== 0 || r.mode !== mode || r.originalGraphDigest !== intent.graphDigest || !sameInstant(clock, run.createdAt) || !['proposed', 'stopped', 'blocked', 'unsupported'].includes(String(r.status))) return fail();
  const selectedVersionId = id(top.selectedVersionId); const graphDigest = hash(r.graphDigest); const captureDigest = hash(r.captureDigest);
  if (mode === 'reproduce' && (selectedVersionId !== intent.versionId || graphDigest !== intent.graphDigest)) return fail();
  const reportId = await identity(['overlaid.engine.rehearsal.v1', graphDigest, intent.graphDigest, run.occurrenceId, mode, captureDigest, '100']);
  if (r.id !== reportId || !Array.isArray(r.steps) || !r.steps.length || r.steps.length > 100 || !Array.isArray(r.proposals) || r.proposals.length > 100) return fail();
  const droppedSteps = integer(r.droppedSteps); const total = r.steps.length + droppedSteps;
  if (total > 10000) return fail();
  const statusReasons: Record<string, string[]> = { proposed: ['supported_flow'], stopped: ['explicit_stop'], unsupported: ['unsupported_node'], blocked: [...admissionReasons.filter(reason => reason !== 'matched'), 'condition_unavailable'] };
  if (!statusReasons[String(r.status)].includes(String(r.reason))) return fail();
  const steps = r.steps.map((raw, index) => {
    const s = record(raw); const sequence = integer(s.sequence, 1);
    if (sequence !== index + 1 || !['visited', 'checked', 'proposed', 'unsupported', 'stopped'].includes(String(s.outcome))) return fail();
    const step = { sequence, parent: integer(s.parent, 0, sequence - 1), automationId: id(s.automationId), versionId: id(s.versionId), nodeId: code(s.nodeId), kind: code(s.kind), outcome: String(s.outcome), reason: code(s.reason) };
    return validStep(step) ? step : fail();
  });
  if (steps[0].kind !== 'admission' || steps[0].automationId !== intent.automationId || steps[0].versionId !== selectedVersionId) return fail();
  const seen = new Set<number>(); let bytes = 0; let previous = 0;
  const proposals = r.proposals.map(raw => {
    const p = record(raw); const sequence = integer(p.sequence, 1, total);
    // The existing Go transport encodes a nil, zero-argument slice as null.
    const args = p.opaqueArguments === null ? [] : p.opaqueArguments;
    if (seen.has(sequence) || sequence <= previous || !Array.isArray(args) || args.length > 32) return fail();
    previous = sequence;
    seen.add(sequence);
    const names = new Set<string>();
    const opaqueArguments = args.map(raw => {
      const a = record(raw); const name = code(a.name);
      if (names.has(name) || typeof a.value !== 'string') return fail();
      names.add(name); bytes += new TextEncoder().encode(name + a.value).length;
      if (bytes > 65536) return fail();
      return { name, value: a.value };
    });
    const proposal = { sequence, automationId: id(p.automationId), versionId: id(p.versionId), nodeId: code(p.nodeId), declaredCapability: code(p.declaredCapability), opaqueArguments };
    const step = steps[sequence - 1];
    if (step && (step.outcome !== 'proposed' || step.automationId !== proposal.automationId || step.versionId !== proposal.versionId || step.nodeId !== proposal.nodeId)) return fail();
    return proposal;
  });
  if (steps.some(step => step.outcome === 'proposed' && !seen.has(step.sequence))) return fail();
  return { id: reportId, mode, selectedVersionId, run, graphDigest, captureDigest, clock, status: String(r.status), reason: code(r.reason), steps, droppedSteps, proposals };
}
