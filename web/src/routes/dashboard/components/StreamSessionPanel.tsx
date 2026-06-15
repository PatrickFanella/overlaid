import { useState } from 'react';
import { endStreamSession, fetchStreamSessionReport, startStreamSession, type StreamSession, type StreamSessionReport, type StreamSessionResponse } from '../../../lib/api';
import { createRehearsalSteps } from '../../../lib/rehearsal';
import { formatSessionReportMarkdown, formatSessionReportSummary, sessionReportFilename } from '../../../lib/sessionReports';
import { EventHistoryPanel } from './EventHistoryPanel';

type StreamSessionPanelProps = {
  apiBaseUrl: string;
  operatorSecret: string;
  status: StreamSessionResponse | null;
  loading?: boolean;
  error?: string;
  onChange: (status: StreamSessionResponse) => void;
};

export function StreamSessionPanel({ apiBaseUrl, operatorSecret, status, loading = false, error = '', onChange }: StreamSessionPanelProps) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [report, setReport] = useState<StreamSessionReport | null>(null);
  const [reportError, setReportError] = useState('');
  const [preflightSteps, setPreflightSteps] = useState<Record<string, StepState>>({});

  async function start(kind: 'rehearsal' | 'live') {
    setBusy(true);
    setMessage('Starting session…');
    try {
      const session = await startStreamSession({ title: kind === 'live' ? 'Live stream' : 'Rehearsal', kind }, apiBaseUrl, operatorSecret);
      onChange({ current: session, recent: [session, ...(status?.recent ?? []).filter((item) => item.id !== session.id)].slice(0, 8) });
      setReport(null);
      setMessage(`${session.title} started.`);
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Unable to start session.');
    } finally {
      setBusy(false);
    }
  }

  async function end() {
    setBusy(true);
    setMessage('Ending session…');
    try {
      const ended = await endStreamSession(apiBaseUrl, operatorSecret);
      const endedSession = isStreamSession(ended) ? ended : undefined;
      const recent = endedSession ? [endedSession, ...(status?.recent ?? []).filter((item) => item.id !== endedSession.id)].slice(0, 8) : status?.recent ?? [];
      onChange({ recent });
      if (endedSession) {
        void loadReport(endedSession.id);
      }
      setMessage(endedSession ? `${endedSession.title} ended.` : 'No active session to end.');
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Unable to end session.');
    } finally {
      setBusy(false);
    }
  }

  async function loadReport(sessionId: string) {
    setBusy(true);
    setReportError('');
    setMessage('Loading session report…');
    try {
      const next = await fetchStreamSessionReport(sessionId, 100, apiBaseUrl, operatorSecret);
      setReport(next);
      setMessage(`${next.session.title} report loaded.`);
    } catch (err) {
      setReportError(err instanceof Error ? err.message : 'Unable to load session report.');
      setMessage('');
    } finally {
      setBusy(false);
    }
  }

  async function copyLatestReportSummary(sessionId: string) {
    setBusy(true);
    setReportError('');
    setMessage('Copying latest report summary…');
    try {
      const next = report?.session.id === sessionId ? report : await fetchStreamSessionReport(sessionId, 100, apiBaseUrl, operatorSecret);
      setReport(next);
      await copyText(formatSessionReportSummary(next));
      setMessage('Latest report summary copied.');
    } catch (err) {
      setReportError(err instanceof Error ? err.message : 'Unable to copy report summary.');
      setMessage('');
    } finally {
      setBusy(false);
    }
  }

  async function runPreflightRehearsal() {
    setBusy(true);
    setReport(null);
    setReportError('');
    setPreflightSteps({});
    setMessage('Starting preflight rehearsal…');

    let session: StreamSession | null = null;
    const steps = createRehearsalSteps(apiBaseUrl, operatorSecret);
    const stepState: Record<string, StepState> = {};

    try {
      const startedSession = await startStreamSession({ title: 'Preflight rehearsal', kind: 'rehearsal', notes: 'One-click dashboard preflight run' }, apiBaseUrl, operatorSecret);
      session = startedSession;
      onChange({ current: startedSession, recent: [startedSession, ...(status?.recent ?? []).filter((item) => item.id !== startedSession.id)].slice(0, 8) });

      for (const step of steps) {
        stepState[step.id] = 'running';
        setPreflightSteps({ ...stepState });
        setMessage(`Running ${step.label}…`);
        await step.run();
        stepState[step.id] = 'pass';
        setPreflightSteps({ ...stepState });
      }

      const ended = await endStreamSession(apiBaseUrl, operatorSecret);
      const endedSession = isStreamSession(ended) ? ended : session;
      const recent = [endedSession, ...(status?.recent ?? []).filter((item) => item.id !== endedSession.id)].slice(0, 8);
      onChange({ recent });
      await loadReport(endedSession.id);
      setMessage('Preflight rehearsal complete. Report loaded.');
    } catch (err) {
      const runningStep = Object.entries(stepState).find(([, state]) => state === 'running')?.[0];
      if (runningStep) {
        stepState[runningStep] = 'fail';
        setPreflightSteps({ ...stepState });
      }
      if (session) {
        try {
          await endStreamSession(apiBaseUrl, operatorSecret);
          await loadReport(session.id);
        } catch {
          // Keep the original preflight failure visible.
        }
      }
      setMessage(err instanceof Error ? `Preflight failed: ${err.message}` : 'Preflight failed.');
    } finally {
      setBusy(false);
    }
  }

  const current = status?.current;
  const recent = status?.recent ?? [];
  const latestSession = recent[0] ?? current;
  const sessionBlockedReason = !operatorSecret
    ? 'Operator secret is missing. Open Config and save the secret before running protected session actions.'
    : status?.loadError
      ? `Stream sessions unavailable: ${status.loadError}`
      : '';
  const startBlockedReason = sessionBlockedReason || (current ? 'End the current session before starting another.' : busy ? 'A session action is already running.' : '');
  const endBlockedReason = sessionBlockedReason || (!current ? 'No active session to end.' : busy ? 'A session action is already running.' : '');
  const preflightBlockedReason = startBlockedReason;

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="hud-label">stream session</p>
          <h2 className="hud-section-title">Rehearsal audit</h2>
          <p className="hud-copy mt-1 text-xs text-zinc-300">Start a rehearsal/live session so new events are tagged for post-stream review.</p>
        </div>
        <span className="hud-chip hud-chip--square">{loading ? 'loading' : current ? 'active' : 'idle'}</span>
      </div>

      {error || status?.loadError ? <p className="mt-3 text-sm text-rose-300">{error || status?.loadError}</p> : null}
      {latestSession ? <LatestReportSummary session={latestSession} report={report?.session.id === latestSession.id ? report : null} busy={busy} onOpen={() => void loadReport(latestSession.id)} onCopy={() => void copyLatestReportSummary(latestSession.id)} /> : null}
      {current ? <SessionCard session={current} label="Current" /> : <p className="mt-3 text-sm text-zinc-400">No active session. Start one before rehearsal or going live.</p>}

      <div className="mt-3 flex flex-wrap gap-2">
        <button className="hud-action-button hud-action-button--compact" type="button" disabled={!!startBlockedReason} title={startBlockedReason || undefined} onClick={() => void start('rehearsal')}>
          Start rehearsal
        </button>
        <button className="hud-action-button hud-action-button--compact" type="button" disabled={!!startBlockedReason} title={startBlockedReason || undefined} onClick={() => void start('live')}>
          Start live
        </button>
        <button className="hud-action-button hud-action-button--compact" type="button" disabled={!!endBlockedReason} title={endBlockedReason || undefined} onClick={() => void end()}>
          End session
        </button>
        <button className="hud-action-button hud-action-button--compact" type="button" disabled={!!preflightBlockedReason} title={preflightBlockedReason || undefined} onClick={() => void runPreflightRehearsal()}>
          Run preflight rehearsal
        </button>
      </div>
      {startBlockedReason || endBlockedReason ? (
        <p className="mt-2 text-xs text-zinc-400">{current ? endBlockedReason : startBlockedReason}</p>
      ) : null}
      {message ? <p className="mt-3 text-xs text-zinc-300">{message}</p> : null}

      {Object.keys(preflightSteps).length > 0 ? (
        <div className="mt-3 grid gap-2 md:grid-cols-5">
          {createRehearsalSteps(apiBaseUrl, operatorSecret).map((step) => (
            <span key={step.id} className="hud-chip hud-chip--square">
              {step.label}: {preflightSteps[step.id] || 'pending'}
            </span>
          ))}
        </div>
      ) : null}

      {recent.length > 0 ? (
        <div className="mt-4 grid gap-2 md:grid-cols-2">
          {recent.slice(0, 4).map((session) => (
            <button key={session.id} className="text-left" type="button" disabled={busy} onClick={() => void loadReport(session.id)}>
              <SessionCard session={session} label={session.endedAt ? 'Recent' : 'Open'} />
            </button>
          ))}
        </div>
      ) : null}
      {reportError ? <p className="mt-3 text-sm text-rose-300">{reportError}</p> : null}
      {report ? (
        <div className="mt-4 grid gap-3">
          <div className="hud-panel hud-corners hud-panel--flat hud-compact-card">
            <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
              <div>
                <p className="hud-kicker">Session report · {report.session.id.slice(0, 8)}</p>
                <p className="mt-1 text-sm font-bold text-white">{report.session.title}</p>
                <p className="mt-1 text-xs text-zinc-400">{report.events.length} visible events · {report.session.eventCount} total tagged events · {report.snapshots.length} readiness snapshots</p>
              </div>
              <div className="flex flex-wrap gap-2">
                <button className="hud-action-button hud-action-button--compact" type="button" onClick={() => downloadReport(report, 'md')}>Export Markdown</button>
                <button className="hud-action-button hud-action-button--compact" type="button" onClick={() => downloadReport(report, 'json')}>Export JSON</button>
              </div>
            </div>
          </div>
          {report.snapshots.length > 0 ? <ReadinessSnapshotGrid snapshots={report.snapshots} /> : null}
          <EventHistoryPanel events={report.events} />
        </div>
      ) : null}
    </section>
  );
}

type StepState = 'pending' | 'running' | 'pass' | 'fail';

function SessionCard({ session, label }: { session: StreamSession; label: string }) {
  return (
    <article className="hud-panel hud-corners hud-panel--flat hud-compact-card">
      <p className="hud-kicker">{label} · {session.kind}</p>
      <p className="mt-1 text-sm font-bold text-white">{session.title}</p>
      <p className="mt-1 text-xs text-zinc-400">Started {new Date(session.startedAt).toLocaleString()}</p>
      {session.endedAt ? <p className="mt-1 text-xs text-zinc-400">Ended {new Date(session.endedAt).toLocaleString()}</p> : null}
      <span className="hud-badge mt-2 inline-flex">{session.eventCount} events</span>
    </article>
  );
}

function LatestReportSummary({ session, report, busy, onOpen, onCopy }: { session: StreamSession; report: StreamSessionReport | null; busy: boolean; onOpen: () => void; onCopy: () => void }) {
  const degradedSnapshots = report?.snapshots.filter((snapshot) => snapshot.status !== 'ok').length ?? 0;
  return (
    <article className="hud-panel hud-corners hud-panel--flat hud-compact-card mt-3 hud-accent-cyan">
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div>
          <p className="hud-kicker">Latest report · {session.id.slice(0, 8)}</p>
          <p className="mt-1 text-sm font-bold text-white">{session.title}</p>
          <p className="mt-1 text-xs text-zinc-400">
            {session.eventCount} tagged events · {session.endedAt ? `ended ${new Date(session.endedAt).toLocaleString()}` : 'still open'}
            {report ? ` · ${report.snapshots.length} snapshots · ${degradedSnapshots} degraded` : ''}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button className="hud-action-button hud-action-button--compact" type="button" disabled={busy} onClick={onOpen}>
            Open latest report
          </button>
          <button className="hud-action-button hud-action-button--compact" type="button" disabled={busy} onClick={onCopy}>
            Copy summary
          </button>
        </div>
      </div>
    </article>
  );
}

function ReadinessSnapshotGrid({ snapshots }: { snapshots: StreamSessionReport['snapshots'] }) {
  return (
    <div className="grid gap-2 md:grid-cols-2">
      {snapshots.map((snapshot) => {
        const degraded = snapshot.checks.filter((check) => check.status !== 'ok');
        return (
          <article key={snapshot.id} className="hud-panel hud-corners hud-panel--flat hud-compact-card">
            <p className="hud-kicker">{snapshot.phase} readiness · {snapshot.status}</p>
            <p className="mt-1 text-xs text-zinc-400">Captured {new Date(snapshot.capturedAt).toLocaleString()}</p>
            {degraded.length > 0 ? (
              <ul className="mt-2 grid gap-1 text-xs text-amber-200">
                {degraded.slice(0, 4).map((check) => (
                  <li key={`${snapshot.id}-${check.name}`}>{check.name}: {check.hint || check.status}</li>
                ))}
              </ul>
            ) : (
              <p className="mt-2 text-xs text-emerald-200">All checks OK.</p>
            )}
          </article>
        );
      })}
    </div>
  );
}

function isStreamSession(value: StreamSession | StreamSessionResponse): value is StreamSession {
  return 'id' in value;
}

async function copyText(value: string) {
  if (typeof navigator !== 'undefined' && navigator.clipboard) {
    await navigator.clipboard.writeText(value);
    return;
  }
  throw new Error('Clipboard is unavailable in this browser context.');
}

function downloadReport(report: StreamSessionReport, format: 'json' | 'md') {
  const content = format === 'json' ? `${JSON.stringify(report, null, 2)}\n` : formatSessionReportMarkdown(report);
  const type = format === 'json' ? 'application/json' : 'text/markdown';
  const blob = new Blob([content], { type });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = sessionReportFilename(report, format);
  link.click();
  URL.revokeObjectURL(url);
}
