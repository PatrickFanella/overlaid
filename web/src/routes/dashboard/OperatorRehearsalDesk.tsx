import { useEffect, useRef, useState } from 'react';
import { versionRequest, type VersionScope } from '../../lib/automationVersions';
import { decodeOperatorRehearsal, decodeOperatorRun, decodeRehearsalIntent, rehearsalStorageKey, type OperatorRehearsal, type OperatorRun, type RehearsalIntent, type RehearsalMode } from '../../lib/operatorRehearsal';
import { MetadataTraceSteps } from './MetadataTraceSteps';
import { OperatorTraceDesk } from './OperatorTraceDesk';
import { Empty, KeyValues, Notice, Panel, Tag } from './ui';

export type RehearsalCandidate = { automationId: string; versionId: string; documentDigest: string; graphDigest: string };

export function OperatorRehearsalDesk({ scope, candidate, admissionBlocked }: { scope: VersionScope; candidate: RehearsalCandidate | null; admissionBlocked: boolean }) {
  const key = rehearsalStorageKey(scope);
  const [initial] = useState(() => { try { return { intent: decodeRehearsalIntent(sessionStorage.getItem(key)), error: '' }; } catch (e) { return { intent: null, error: e instanceof Error ? e.message : 'Tab storage is unavailable.' }; } });
  const [intent, setIntent] = useState<RehearsalIntent | null>(initial.intent);
  const [storageError, setStorageError] = useState(initial.error);
  const [error, setError] = useState('');
  const [run, setRun] = useState<OperatorRun | null>(null);
  const [report, setReport] = useState<OperatorRehearsal | null>(null);
  const [busy, setBusy] = useState(false);
  const request = useRef<AbortController | null>(null);
  const mounted = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; request.current?.abort(); }; }, []);
  function persist(next: RehearsalIntent | null) {
    try {
      if (next) { const raw = JSON.stringify(next); decodeRehearsalIntent(raw); sessionStorage.setItem(key, raw); } else sessionStorage.removeItem(key);
      setIntent(next); setStorageError(''); return true;
    } catch { setStorageError('Rehearsal intent storage is unavailable. Admission is blocked until the original intent can be saved.'); return false; }
  }
  function active(signal: AbortSignal) {
    if (signal.aborted || !mounted.current || request.current?.signal !== signal) throw new Error('Rehearsal request canceled. Check the original occurrence before another admission.');
  }
  async function perform(job: (signal: AbortSignal) => Promise<void>) {
    if (request.current) return;
    const controller = new AbortController(); request.current = controller; setBusy(true); setError(''); setReport(null);
    const timeout = window.setTimeout(() => controller.abort(), 15000);
    try { await job(controller.signal); }
    catch (e) { if (mounted.current) setError(controller.signal.aborted ? 'Rehearsal request canceled or timed out. The original intent is retained; check its saved occurrence.' : e instanceof Error ? e.message : 'Rehearsal operation failed.'); }
    finally { window.clearTimeout(timeout); if (request.current === controller) { request.current = null; if (mounted.current) setBusy(false); } }
  }
  function prepare() {
    if (busy || intent || storageError || !candidate || admissionBlocked) return;
    const next: RehearsalIntent = { format: 1, phase: 'prepared', requestId: crypto.randomUUID(), ...candidate };
    if (persist(next)) { setRun(null); setReport(null); setError(''); }
  }
  async function admit(signal: AbortSignal) {
    if (!intent || intent.phase !== 'prepared' || storageError || admissionBlocked) return;
    const frozen: RehearsalIntent = { ...intent, phase: 'submitted' };
    if (!persist(frozen)) return;
    const raw = await versionRequest(scope, 'admit', { requestId: frozen.requestId, automationId: frozen.automationId, expectedVersionId: frozen.versionId }, signal);
    const receipt = await decodeOperatorRun(raw, scope, frozen); active(signal); setRun(receipt);
  }
  async function lookup(signal: AbortSignal) {
    if (!intent) return;
    setRun(null);
    const raw = await versionRequest(scope, 'lookup', { requestId: intent.requestId }, signal);
    const receipt = await decodeOperatorRun(raw, scope, intent); active(signal);
    if (persist({ ...intent, phase: 'submitted' })) setRun(receipt);
  }
  async function rehearse(mode: RehearsalMode, signal: AbortSignal) {
    if (!intent || !run) return;
    const raw = await versionRequest(scope, 'rehearse', { requestId: intent.requestId, mode }, signal);
    const result = await decodeOperatorRehearsal(raw, scope, intent, run, mode); active(signal); setReport(result);
  }
  function clear() {
    if (window.confirm('Clear this tab’s rehearsal intent? This does not cancel, delete, refund or undo retained admission. Keep the original request ID if its outcome is unresolved.')) {
      if (persist(null)) { setRun(null); setReport(null); setError(''); }
    }
  }
  return <Panel title="Operator rehearsal" description="Retain an operator occurrence, then inspect effect-free flow. Admission reserves retained metadata capacity and the declared cooldown. It does not execute actions or enqueue delivery jobs.">
    {storageError ? <Notice tone="bad" role="alert">{storageError} <button className="desk-btn desk-btn--small" disabled={busy} onClick={clear}>Acknowledge unreadable rehearsal state</button></Notice> : null}
    {error ? <Notice tone="bad" role="alert">{error}</Notice> : null}
    {busy ? <p role="status">Checking this retained occurrence…</p> : null}
    {admissionBlocked ? <Notice>Wait for version operations to finish and resolve any version intent or storage error before preparing or admitting new rehearsal metadata. Original-occurrence checks and rehearsal remain read-only.</Notice> : null}
    {!intent ? <div className="grid gap-3"><Empty>{candidate ? 'Prepare a stable request for the loaded schema-2 dashboard version. The server checks its current selection and admission policy.' : 'Load a schema-2 dashboard version to prepare an operator occurrence. Existing tab intents can be recovered independently of the loaded version.'}</Empty><button className="desk-btn w-fit" disabled={busy || !!storageError || !candidate || admissionBlocked} onClick={prepare}>Prepare rehearsal occurrence</button></div> : <div className="grid min-w-0 gap-3">
      <div className="break-all"><KeyValues items={[
        ['Request UUID', intent.requestId], ['Original automation', intent.automationId], ['Original version', intent.versionId], ['Original graph digest', intent.graphDigest], ['Tab intent', run ? 'Accepted metadata verified' : intent.phase === 'prepared' ? 'Prepared; no admission attempt sent' : 'Submitted; check original saved occurrence'],
      ]} /></div>
      <div className="flex flex-wrap gap-2"><button className="desk-btn desk-btn--primary" disabled={busy || !!storageError || intent.phase !== 'prepared' || admissionBlocked} onClick={() => void perform(admit)}>Admit occurrence metadata</button><button className="desk-btn" disabled={busy} onClick={() => void perform(lookup)}>Check original occurrence</button><button className="desk-btn" disabled={busy} onClick={clear}>Acknowledge and clear rehearsal intent</button></div>
      <p className="text-sm">The initiating account/workspace/request/version binding is fixed. Reload requires checking this original request. Errors never generate a replacement request or automatically retry admission. Clearing changes tab bookkeeping only.</p>
    </div>}
    {run ? <div className="mt-4 grid gap-3"><Tag>Retained metadata accepted · no execution</Tag><div className="break-all"><KeyValues items={[
      ['Original run', run.runId], ['Original occurrence', run.occurrenceId], ['Captured database clock', run.createdAt], ['Original deadline', run.deadline],
    ]} /></div><p className="text-sm">Capture format 1 contains empty facts and seed zero as a format placeholder. It records no provider facts, random choice or rendered variables. The original deadline does not forbid a retained effect-free read.</p><div className="flex flex-wrap gap-2"><button className="desk-btn" disabled={busy} onClick={() => void perform(signal => rehearse('reproduce', signal))}>Reproduce original capture</button><button className="desk-btn" disabled={busy} onClick={() => void perform(signal => rehearse('current_config', signal))}>Rehearse current configuration</button></div></div> : null}
    {report ? <RehearsalReport report={report} /> : null}
    <OperatorTraceDesk scope={scope} source={intent} run={run} report={report} blocked={busy || admissionBlocked || !!storageError} />
  </Panel>;
}

export function RehearsalReport({ report }: { report: OperatorRehearsal }) {
  return <section aria-label="Retained rehearsal report" className="mt-5 grid min-w-0 gap-3">
    <Notice role="status">Rehearsal: {report.status} · {report.reason}. This report is not execution permission or a delivery receipt.</Notice>
    <div className="break-all"><KeyValues items={[
      ['Mode', report.mode], ['Selected version', report.selectedVersionId], ['Report ID', report.id], ['Original graph', report.run.graphDigest], ['Rehearsed graph', report.graphDigest], ['Capture digest', report.captureDigest], ['Original clock', report.clock],
    ]} /></div>
    <MetadataTraceSteps steps={report.steps} droppedSteps={report.droppedSteps} />
    <h3 className="desk-sub__title">Opaque action proposals</h3><p className="text-sm">Arguments are authored text, not rendered variables or transport payloads. Diagnostic proposals in blocked or unsupported reports cannot be executed.</p>
    {!report.proposals.length ? <Empty>No action proposals in this report.</Empty> : <ol className="grid gap-3">{report.proposals.map(proposal => <li className="desk-sub min-w-0 break-all" key={proposal.sequence}><p className="text-sm">Step {proposal.sequence} · {proposal.nodeId} · declared {proposal.declaredCapability}</p><pre className="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs">{JSON.stringify(proposal.opaqueArguments, null, 2)}</pre></li>)}</ol>}
    <p className="text-sm">This live report stays in memory only. Use the separate save control for fixed metadata; browsing past occurrences remains separate work.</p>
  </section>;
}
