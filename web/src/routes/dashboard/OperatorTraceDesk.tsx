import { useEffect, useRef, useState } from 'react';
import { versionRequest, type VersionScope } from '../../lib/automationVersions';
import { decodeOperatorRun, type OperatorRehearsal, type OperatorRun, type RehearsalIntent } from '../../lib/operatorRehearsal';
import { decodeSavedOperatorTrace, decodeTraceReference, traceStorageKey, type SavedOperatorTrace, type TraceReference } from '../../lib/operatorTraces';
import { SavedTraceSummary } from './SavedTraceSummary';
import { OperatorTraceHistory } from './OperatorTraceHistory';
import { Empty, KeyValues, Notice } from './ui';

export function OperatorTraceDesk({ scope, source, run, report, blocked }: { scope: VersionScope; source: RehearsalIntent | null; run: OperatorRun | null; report: OperatorRehearsal | null; blocked: boolean }) {
  const key = traceStorageKey(scope);
  const [initial] = useState(() => { try { return { ref: decodeTraceReference(sessionStorage.getItem(key)), error: '' }; } catch (e) { return { ref: null, error: e instanceof Error ? e.message : 'Trace intent storage is unavailable.' }; } });
  const [reference, setReference] = useState<TraceReference | null>(initial.ref);
  const [storageError, setStorageError] = useState(initial.error);
  const [saved, setSaved] = useState<SavedOperatorTrace | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const controller = useRef<AbortController | null>(null);
  const mounted = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; controller.current?.abort(); }; }, []);
  function persist(ref: TraceReference | null) {
    try {
      if (ref) { const raw = JSON.stringify(ref); decodeTraceReference(raw); sessionStorage.setItem(key, raw); } else sessionStorage.removeItem(key);
      setReference(ref); setStorageError(''); return true;
    } catch { setStorageError('Trace intent storage is unavailable. No new trace save can start.'); return false; }
  }
  function active(signal: AbortSignal) { if (signal.aborted || !mounted.current || controller.current?.signal !== signal) throw new Error('Trace request canceled. Check the original saved trace identity.'); }
  async function perform(job: (signal: AbortSignal) => Promise<void>) {
    if (controller.current) return;
    const current = new AbortController(); controller.current = current; setBusy(true); setError(''); setSaved(null);
    const timeout = window.setTimeout(() => current.abort(), 15000);
    try { await job(current.signal); }
    catch (e) { if (mounted.current) setError(current.signal.aborted ? 'Trace request canceled or timed out. The original trace intent is retained; check saved state.' : e instanceof Error ? e.message : 'Saved trace operation failed.'); }
    finally { window.clearTimeout(timeout); if (controller.current === current) { controller.current = null; if (mounted.current) setBusy(false); } }
  }
  async function save(signal: AbortSignal) {
    if (!source || !run || !report || reference || storageError || blocked || report.run.runId !== run.runId) return;
    const ref: TraceReference = { format: 1, source: { ...source, phase: 'submitted' }, traceId: report.id, mode: report.mode, versionId: report.selectedVersionId, graphDigest: report.graphDigest, captureDigest: report.captureDigest };
    if (!persist(ref)) return;
    const raw = await versionRequest(scope, 'save-trace', { requestId: ref.source.requestId, mode: ref.mode, expectedVersionId: ref.versionId }, signal);
    const trace = await decodeSavedOperatorTrace(raw, scope, ref, run); active(signal); setSaved(trace);
  }
  async function check(signal: AbortSignal) {
    if (!reference) return;
    const rawRun = await versionRequest(scope, 'lookup', { requestId: reference.source.requestId }, signal);
    const original = await decodeOperatorRun(rawRun, scope, reference.source); active(signal);
    const raw = await versionRequest(scope, 'load-trace', { requestId: reference.source.requestId, traceId: reference.traceId }, signal);
    const trace = await decodeSavedOperatorTrace(raw, scope, reference, original); active(signal); setSaved(trace);
  }
  function clear() {
    if (controller.current) return;
    if (window.confirm('Clear this tab’s trace handle? This does not delete the saved trace, cancel a save, refund capacity or establish an unresolved outcome. Keep the original request and trace IDs for recovery.')) {
      if (persist(null)) { setSaved(null); setError(''); }
    }
  }
  return <section aria-label="Saved rehearsal trace" className="mt-5 grid min-w-0 gap-3 border-t border-white/10 pt-4">
    <h3 className="desk-sub__title">Saved rehearsal trace</h3>
    <p className="text-sm">Save fixed trace metadata from the validated report. The server regenerates the trace; no arguments, facts or proposals are submitted or stored. Saving uses retained trace capacity and does not execute actions.</p>
    {storageError ? <Notice tone="bad" role="alert">{storageError} <button className="desk-btn desk-btn--small" disabled={busy} onClick={clear}>Acknowledge unreadable trace state</button></Notice> : null}
    {error ? <Notice tone="bad" role="alert">{error}</Notice> : null}
    {busy ? <p role="status">Checking the original trace identity…</p> : null}
    <button className="desk-btn w-fit" disabled={busy || blocked || !!storageError || !!reference || !source || !run || !report} onClick={() => void perform(save)}>Save rehearsal metadata trace</button>
    {reference ? <><div className="break-all"><KeyValues items={[
      ['Original request', reference.source.requestId], ['Original trace ID', reference.traceId], ['Trace mode', reference.mode], ['Trace version', reference.versionId], ['Tab trace state', saved ? 'Saved metadata verified' : 'Outcome unverified; check original saved trace'],
    ]} /></div><div className="flex flex-wrap gap-2"><button className="desk-btn" disabled={busy} onClick={() => void perform(check)}>Check original saved trace</button><button className="desk-btn" disabled={busy} onClick={clear}>Acknowledge and clear trace handle</button></div><p className="text-sm">This tab retains one original trace handle, independently of the currently loaded version or rehearsal. Checks use only original-occurrence lookup and trace loading. Errors never repeat the save. Clearing affects local bookkeeping only; retain the IDs before clearing an unresolved handle.</p></> : <Empty>Rehearse an accepted occurrence to choose a trace to save. Existing saved handles can be checked independently after reload.</Empty>}
    {saved ? <SavedTraceSummary trace={saved} /> : null}
    <OperatorTraceHistory key={JSON.stringify(reference?.source || (source?.phase === 'submitted' ? source : null))} scope={scope} source={reference?.source || (source?.phase === 'submitted' ? source : null)} />
  </section>;
}
