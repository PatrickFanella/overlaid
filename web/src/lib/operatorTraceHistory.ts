import { versionID, type VersionScope } from './automationVersions';
import { decodeOperatorRun, operatorTimestamp, sameOperatorInstant, type OperatorRun, type RehearsalIntent, type RehearsalMode } from './operatorRehearsal';
import { decodeSavedOperatorTrace, type TraceReference } from './operatorTraces';
export type TraceSelection = { traceId: string; versionId: string; mode: RehearsalMode; digest: string; createdAt: string };
export type TraceCatalogue = { run: OperatorRun; traces: TraceSelection[] };
const fail = (): never => { throw new Error('Invalid saved trace catalogue. Refresh the original occurrence metadata.'); };
function record(v: unknown): Record<string, unknown> { return v && typeof v === 'object' && !Array.isArray(v) ? v as Record<string, unknown> : fail(); }
function hash(v: unknown): string { return typeof v === 'string' && /^[a-f0-9]{64}$/.test(v) ? v : fail(); }
function compareTime(a: string, b: string) {
  const milliseconds = Date.parse(a) - Date.parse(b);
  if (milliseconds) return milliseconds;
  const nanos = (v: string) => Number((v.match(/\.(\d+)/)?.[1] || '').padEnd(9, '0').slice(3));
  return nanos(a) - nanos(b);
}
export async function decodeTraceCatalogue(value: unknown, scope: VersionScope, source: RehearsalIntent): Promise<TraceCatalogue> {
  const top = record(value);
  if (source.phase !== 'submitted' || !Array.isArray(top.traces) || top.traces.length > 8) return fail();
  const run = await decodeOperatorRun(top.run, scope, source);
  if (!run.unchanged) return fail();
  const ids = new Set<string>(); const selections = new Set<string>();
  const traces: TraceSelection[] = [];
  for (const raw of top.traces) {
    const item = record(raw);
    if (!versionID(item.versionId) || !['reproduce', 'current_config'].includes(String(item.mode))) return fail();
    const trace: TraceSelection = { traceId: hash(item.traceId), versionId: item.versionId, mode: item.mode as RehearsalMode, digest: hash(item.digest), createdAt: operatorTimestamp(item.createdAt) };
    if (trace.mode === 'reproduce' && trace.versionId !== source.versionId) return fail();
    const selection = `${trace.mode}:${trace.versionId}`;
    if (ids.has(trace.traceId) || selections.has(selection)) return fail();
    const prior = traces.at(-1);
    if (prior) { const order = compareTime(prior.createdAt, trace.createdAt); if (order < 0 || (order === 0 && prior.traceId <= trace.traceId)) return fail(); }
    ids.add(trace.traceId); selections.add(selection); traces.push(trace);
  }
  return { run, traces };
}
export async function decodeHistoryTrace(value: unknown, scope: VersionScope, source: RehearsalIntent, catalogue: TraceCatalogue, selected: TraceSelection) {
  // Only an entry from this validated in-memory catalogue can select a load.
  if (!catalogue.traces.includes(selected)) return fail();
  const top = record(value); const d = record(top.document);
  const reference: TraceReference = { format: 1, source, traceId: selected.traceId, versionId: selected.versionId, mode: selected.mode, graphDigest: hash(d.graphDigest), captureDigest: hash(d.captureDigest) };
  if (reference.mode === 'reproduce' && reference.graphDigest !== source.graphDigest) return fail();
  const trace = await decodeSavedOperatorTrace(value, scope, reference, catalogue.run);
  if (trace.digest !== selected.digest || !sameOperatorInstant(trace.createdAt, selected.createdAt)) return fail();
  return trace;
}
