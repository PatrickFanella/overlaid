import { useEffect, useRef, useState } from 'react';
import { versionRequest, type VersionScope } from '../../lib/automationVersions';
import type { RehearsalIntent } from '../../lib/operatorRehearsal';
import { decodeHistoryTrace, decodeTraceCatalogue, type TraceCatalogue, type TraceSelection } from '../../lib/operatorTraceHistory';
import type { SavedOperatorTrace } from '../../lib/operatorTraces';
import { SavedTraceSummary } from './SavedTraceSummary';
import { Empty, Notice } from './ui';

export function OperatorTraceHistory({ scope, source }: { scope: VersionScope; source: RehearsalIntent | null }) {
  const [catalogue, setCatalogue] = useState<TraceCatalogue | null>(null);
  const [trace, setTrace] = useState<SavedOperatorTrace | null>(null);
  const [error, setError] = useState(''); const [busy, setBusy] = useState(false);
  const request = useRef<AbortController | null>(null); const mounted = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; request.current?.abort(); }; }, []);
  function active(signal: AbortSignal) { if (signal.aborted || !mounted.current || request.current?.signal !== signal) throw new Error('Saved trace history request canceled.'); }
  async function perform(job: (signal: AbortSignal) => Promise<void>) {
    if (request.current) return;
    const controller = new AbortController(); request.current = controller; setBusy(true); setError(''); setTrace(null);
    const timeout = window.setTimeout(() => controller.abort(), 15000);
    try { await job(controller.signal); }
    catch (e) { if (mounted.current) setError(controller.signal.aborted ? 'Saved trace history request canceled or timed out. Refresh the original occurrence.' : e instanceof Error ? e.message : 'Saved trace history is unavailable.'); }
    finally { window.clearTimeout(timeout); if (request.current === controller) { request.current = null; if (mounted.current) setBusy(false); } }
  }
  async function refresh(signal: AbortSignal) {
    setCatalogue(null);
    if (!source) return;
    const raw = await versionRequest(scope, 'list-traces', { requestId: source.requestId }, signal);
    const result = await decodeTraceCatalogue(raw, scope, source); active(signal); setCatalogue(result);
  }
  async function open(selected: TraceSelection, signal: AbortSignal) {
    if (!source || !catalogue || !catalogue.traces.includes(selected)) return;
    const raw = await versionRequest(scope, 'load-trace', { requestId: source.requestId, traceId: selected.traceId }, signal);
    const result = await decodeHistoryTrace(raw, scope, source, catalogue, selected); active(signal); setTrace(result);
  }
  return <section aria-label="Original occurrence trace history" className="mt-3 grid min-w-0 gap-3 border-t border-white/10 pt-4">
    <h3 className="desk-sub__title">Original occurrence trace history</h3>
    <p className="text-sm">Browse saved metadata for the original request below. Opening a snapshot uses reads only and leaves any unresolved save handle intact. Use Check original saved trace to update that handle’s recovery status.</p>
    {source ? <p className="break-all text-sm">History request: {source.requestId}</p> : <Empty>Submit an original rehearsal intent or retain a trace handle to browse its saved selections. The original request is required.</Empty>}
    <button className="desk-btn w-fit" disabled={busy || !source} onClick={() => void perform(refresh)}>Refresh original trace history</button>
    {busy ? <p role="status">Reading saved trace history…</p> : null}
    {error ? <Notice tone="bad" role="alert">{error}</Notice> : null}
    {catalogue ? <><p className="break-all text-sm">Original run: {catalogue.run.runId}. Up to eight saved selections, newest save first; equal times sort by trace ID descending. This is a discovery snapshot; loading still checks the stored body.</p>{!catalogue.traces.length ? <Empty>No saved traces for this original occurrence.</Empty> : <ol className="grid gap-2">{catalogue.traces.map(item => <li key={item.traceId} className="desk-sub min-w-0 break-all text-sm"><p>{item.mode} · version {item.versionId}</p><p>Saved {item.createdAt}</p><p>Trace {item.traceId}</p><p>Digest {item.digest}</p><button className="desk-btn desk-btn--small mt-2" disabled={busy} aria-label={`Open saved trace ${item.traceId}`} onClick={() => void perform(signal => open(item, signal))}>Open saved metadata</button></li>)}</ol>}</> : null}
    {trace ? <SavedTraceSummary trace={trace} /> : null}
    <p className="text-sm">History stays in memory and clears on reload or source/workspace departure. Missing or purged records remain missing. These controls do not save reports, repeat admission, execute actions or change local recovery intents.</p>
  </section>;
}
