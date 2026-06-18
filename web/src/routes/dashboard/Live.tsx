import { useEffect, useState } from 'react';
import {
  fetchEventHistory,
  fetchOBSStatus,
  fetchReadiness,
  fetchStreamSession,
  fetchTwitchStatus,
  type EventHistoryResponse,
  type OBSStatus,
  type ReadinessReport,
  type StreamSessionResponse,
  type TwitchStatus,
} from '../../lib/api';
import { loadDashboardConfig, loadOperatorSecret } from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { loadSessionLog, type SessionLogEntry } from '../../lib/sessionLog';
import { DashboardShell } from './DashboardShell';
import { ConsolePageHeader } from './components/ConsolePageHeader';
import { EventSubDiagnostics } from './components/EventSubDiagnostics';
import { EventHistoryPanel } from './components/EventHistoryPanel';
import { LivePreflight } from './components/LivePreflight';
import { LiveChecklistSummary } from './components/LiveChecklistSummary';
import { RecoveryConsolePanel } from './components/RecoveryConsolePanel';
import { ReadinessPanel } from './components/ReadinessPanel';
import { SessionLogPanel } from './components/SessionLogPanel';
import { BuildInfoPanel } from './components/BuildInfoPanel';
import { StreamSessionPanel } from './components/StreamSessionPanel';

type LiveStatus = {
  twitch: TwitchStatus | null;
  obs: OBSStatus | null;
  readiness: ReadinessReport | null;
  history: EventHistoryResponse | null;
  session: StreamSessionResponse | null;
  loading: boolean;
  error: string;
  readinessError: string;
  historyError: string;
  sessionError: string;
};

export function DashboardLivePage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const config = loadDashboardConfig();
  const operatorSecret = loadOperatorSecret();
  const [sessionLog] = useState<SessionLogEntry[]>(() => loadSessionLog());
  const [status, setStatus] = useState<LiveStatus>({
    twitch: null,
    obs: null,
    readiness: null,
    history: null,
    session: null,
    loading: false,
    error: '',
    readinessError: '',
    historyError: '',
    sessionError: '',
  });

  useEffect(() => {
    let cancelled = false;

    async function load() {
      setStatus((current) => ({ ...current, loading: true, error: '', readinessError: '', historyError: '', sessionError: '' }));

      const [twitchResult, obsResult, readinessResult, historyResult, sessionResult] = await Promise.allSettled([
        fetchTwitchStatus(apiBaseUrl),
        fetchOBSStatus(apiBaseUrl),
        fetchReadiness(apiBaseUrl),
        fetchEventHistory(24, apiBaseUrl),
        fetchStreamSession(apiBaseUrl, operatorSecret),
      ]);
      if (cancelled) return;

      setStatus({
        twitch: twitchResult.status === 'fulfilled' ? twitchResult.value : null,
        obs: obsResult.status === 'fulfilled' ? obsResult.value : null,
        readiness: readinessResult.status === 'fulfilled' ? readinessResult.value : null,
        history: historyResult.status === 'fulfilled' ? historyResult.value : null,
        session: sessionResult.status === 'fulfilled' ? sessionResult.value : null,
        loading: false,
        error: twitchResult.status === 'rejected' || obsResult.status === 'rejected' ? 'Some live status checks are unavailable.' : '',
        readinessError: readinessResult.status === 'rejected' ? 'Readiness check unavailable.' : '',
        historyError: historyResult.status === 'rejected' ? 'Event history unavailable.' : '',
        sessionError: sessionResult.status === 'rejected' ? 'Stream session status unavailable.' : '',
      });
    }

    void load();
    return () => {
      cancelled = true;
    };
  }, [apiBaseUrl, operatorSecret]);

  return (
    <DashboardShell activePath="/dashboard/live">
      <ConsolePageHeader
        eyebrow="Live command"
        title="Live ops cockpit"
        description="Check readiness, recover production issues, review EventSub, and keep session evidence visible before going live."
        accent="emerald"
        chips={[status.loading ? 'checking' : 'checked', 'recovery links ready']}
        actions={status.error ? <span className="hud-chip hud-chip--square hud-accent-amber">{status.error}</span> : null}
      />

      <div className="hud-dashboard-grid hud-dashboard-grid--two">
        <div className="grid gap-3">
          <RecoveryConsolePanel />
          <ReadinessPanel report={status.readiness} loading={status.loading} error={status.readinessError} />
          <LivePreflight twitch={status.twitch} obs={status.obs} config={config} operatorSecret={operatorSecret} />
          <EventSubDiagnostics status={status.twitch} />
        </div>
        <div className="grid gap-3">
          <BuildInfoPanel apiBaseUrl={apiBaseUrl} />
          <StreamSessionPanel apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} status={status.session} loading={status.loading} error={status.sessionError} onChange={(session) => setStatus((current) => ({ ...current, session }))} />
          <LiveChecklistSummary items={config.setupChecklist} />
          <SessionLogPanel entries={sessionLog} />
        </div>
      </div>
      <EventHistoryPanel events={status.history?.events ?? []} loading={status.loading} error={status.historyError} />
    </DashboardShell>
  );
}
