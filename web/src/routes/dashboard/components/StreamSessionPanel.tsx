import { useState } from 'react';
import { endStreamSession, fetchStreamSessionReport, startStreamSession, type StreamSession, type StreamSessionReport, type StreamSessionResponse } from '../../../lib/api';
import { createRehearsalSteps } from '../../../lib/rehearsal';
import { formatSessionReportMarkdown, formatSessionReportSummary, sessionReportFilename } from '../../../lib/sessionReports';
import { EventHistoryPanel } from './EventHistoryPanel';
import { Empty, Notice, Panel, SubPanel, Tag } from '../ui';

type StreamSessionPanelProps = {
  apiBaseUrl: string;
  operatorSecret: string;
  status: StreamSessionResponse | null;
  loading?: boolean;
  error?: string;
  onChange: (status: StreamSessionResponse) => void;
  compact?: boolean;
};

export function StreamSessionPanel({ apiBaseUrl, operatorSecret, status, loading = false, error = '', onChange, compact = false }: StreamSessionPanelProps) {
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
    ? 'Operator secret is missing. Add it in Settings before running protected session actions.'
    : status?.loadError
      ? `Stream sessions unavailable: ${status.loadError}`
      : '';
  const startBlockedReason = sessionBlockedReason || (current ? 'End the current session before starting another.' : busy ? 'A session action is already running.' : '');
  const endBlockedReason = sessionBlockedReason || (!current ? 'No active session to end.' : busy ? 'A session action is already running.' : '');
  const preflightBlockedReason = startBlockedReason;

  const steps = createRehearsalSteps(apiBaseUrl, operatorSecret);
  const blockedReason = current ? endBlockedReason : startBlockedReason;

  return (
    <Panel
      kicker="Stream session"
      title="Rehearsal audit"
      description={compact ? undefined : 'Start a rehearsal or live session so new events are tagged for the post-stream report.'}
      actions={<Tag tone={current ? (current.kind === 'live' ? 'bad' : 'warn') : 'neutral'}>{loading ? 'loading' : current ? `${current.kind} running` : 'idle'}</Tag>}
    >
      {error || status?.loadError ? <Notice tone="bad">{error || status?.loadError}</Notice> : null}

      {current ? (
        <SessionSummary session={current} label="Current" />
      ) : (
        <Empty>No active session. Start one before rehearsing or going live.</Empty>
      )}

      <div className="grid grid-cols-2 gap-2">
        {current ? (
          <button className="desk-btn desk-btn--danger col-span-2" type="button" disabled={!!endBlockedReason} title={endBlockedReason || undefined} onClick={() => void end()}>
            End session
          </button>
        ) : (
          <>
            <button className="desk-btn desk-btn--primary" type="button" disabled={!!startBlockedReason} title={startBlockedReason || undefined} onClick={() => void start('live')}>
              Start live
            </button>
            <button className="desk-btn" type="button" disabled={!!startBlockedReason} title={startBlockedReason || undefined} onClick={() => void start('rehearsal')}>
              Start rehearsal
            </button>
          </>
        )}
        <button className="desk-btn col-span-2" type="button" disabled={!!preflightBlockedReason} title={preflightBlockedReason || undefined} onClick={() => void runPreflightRehearsal()}>
          Run preflight rehearsal
        </button>
      </div>
      {blockedReason && !busy ? <p className="desk-field__hint">{blockedReason}</p> : null}
      {message ? (
        <p className="desk-row__text" role="status">
          {message}
        </p>
      ) : null}

      {Object.keys(preflightSteps).length > 0 ? (
        <ol className="desk-steps" aria-label="Preflight steps">
          {steps.map((step) => {
            const state = preflightSteps[step.id] || 'pending';
            return (
              <li key={step.id}>
                <Tag tone={state === 'pass' ? 'ok' : state === 'fail' ? 'bad' : state === 'running' ? 'warn' : 'neutral'}>
                  {step.label}: {state}
                </Tag>
              </li>
            );
          })}
        </ol>
      ) : null}

      {latestSession && !compact ? <LatestReportSummary session={latestSession} report={report?.session.id === latestSession.id ? report : null} busy={busy} onOpen={() => void loadReport(latestSession.id)} onCopy={() => void copyLatestReportSummary(latestSession.id)} /> : null}

      {recent.length > 0 && !compact ? (
        <SubPanel title="Recent sessions">
          <ul className="grid gap-2 md:grid-cols-2">
            {recent.slice(0, 6).map((session) => (
              <li key={session.id}>
                <button className="desk-btn w-full flex-col items-start" type="button" disabled={busy} onClick={() => void loadReport(session.id)}>
                  <SessionSummary session={session} label={session.endedAt ? 'Recent' : 'Open'} />
                </button>
              </li>
            ))}
          </ul>
        </SubPanel>
      ) : null}
      {compact && latestSession ? (
        <a className="desk-btn desk-btn--small desk-btn--ghost justify-self-start" href="/dashboard/sessions">
          Reports and history
        </a>
      ) : null}
      {reportError ? <Notice tone="bad">{reportError}</Notice> : null}
      {report && !compact ? (
        <div className="grid gap-3">
          <SubPanel title={`Session report · ${report.session.id.slice(0, 8)}`}>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <p className="desk-row__title">{report.session.title}</p>
                <p className="desk-row__meta">
                  {report.events.length} visible events · {report.session.eventCount} tagged events · {report.snapshots.length} readiness snapshots
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                <button className="desk-btn desk-btn--small" type="button" onClick={() => downloadReport(report, 'md')}>
                  Export Markdown
                </button>
                <button className="desk-btn desk-btn--small" type="button" onClick={() => downloadReport(report, 'json')}>
                  Export JSON
                </button>
              </div>
            </div>
          </SubPanel>
          {report.snapshots.length > 0 ? <ReadinessSnapshotGrid snapshots={report.snapshots} /> : null}
          <EventHistoryPanel events={report.events} title="Session events" />
        </div>
      ) : null}
    </Panel>
  );
}

type StepState = 'pending' | 'running' | 'pass' | 'fail';

function SessionSummary({ session, label }: { session: StreamSession; label: string }) {
  return (
    <div className="grid gap-0.5 text-left">
      <p className="desk-panel__kicker">
        {label} · {session.kind}
      </p>
      <p className="desk-row__title">{session.title}</p>
      <p className="desk-row__meta">
        Started {new Date(session.startedAt).toLocaleString()}
        {session.endedAt ? ` · ended ${new Date(session.endedAt).toLocaleString()}` : ''} · {session.eventCount} events
      </p>
    </div>
  );
}

function LatestReportSummary({ session, report, busy, onOpen, onCopy }: { session: StreamSession; report: StreamSessionReport | null; busy: boolean; onOpen: () => void; onCopy: () => void }) {
  const degradedSnapshots = report?.snapshots.filter((snapshot) => snapshot.status !== 'ok').length ?? 0;
  return (
    <SubPanel title={`Latest report · ${session.id.slice(0, 8)}`}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="desk-row__title">{session.title}</p>
          <p className="desk-row__meta">
            {session.eventCount} tagged events · {session.endedAt ? `ended ${new Date(session.endedAt).toLocaleString()}` : 'still open'}
            {report ? ` · ${report.snapshots.length} snapshots · ${degradedSnapshots} degraded` : ''}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button className="desk-btn desk-btn--small" type="button" disabled={busy} onClick={onOpen}>
            Open latest report
          </button>
          <button className="desk-btn desk-btn--small" type="button" disabled={busy} onClick={onCopy}>
            Copy summary
          </button>
        </div>
      </div>
    </SubPanel>
  );
}

function ReadinessSnapshotGrid({ snapshots }: { snapshots: StreamSessionReport['snapshots'] }) {
  return (
    <div className="grid gap-2 md:grid-cols-2">
      {snapshots.map((snapshot) => {
        const degraded = snapshot.checks.filter((check) => check.status !== 'ok');
        return (
          <SubPanel key={snapshot.id} title={`${snapshot.phase} readiness · ${snapshot.status}`}>
            <p className="desk-row__meta">Captured {new Date(snapshot.capturedAt).toLocaleString()}</p>
            {degraded.length > 0 ? (
              <Notice tone="warn">
                <ul>
                  {degraded.slice(0, 4).map((check) => (
                    <li key={`${snapshot.id}-${check.name}`}>
                      {check.name}: {check.hint || check.status}
                    </li>
                  ))}
                </ul>
              </Notice>
            ) : (
              <Notice tone="ok">All checks OK.</Notice>
            )}
          </SubPanel>
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
