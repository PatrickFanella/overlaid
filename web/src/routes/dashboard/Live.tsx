import { useEffect, useState } from 'react';
import {
  fetchEventHistory,
  fetchOBSStatus,
  fetchReadiness,
  fetchTwitchStatus,
  type EventHistoryResponse,
  type OBSStatus,
  type ReadinessReport,
  type TwitchStatus,
} from '../../lib/api';
import { loadDashboardConfig, loadOperatorSecret } from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { loadSessionLog, type SessionLogEntry } from '../../lib/sessionLog';
import { DashboardShell } from './DashboardShell';
import { EventSubDiagnostics } from './components/EventSubDiagnostics';
import { EventHistoryPanel } from './components/EventHistoryPanel';
import { LivePreflight } from './components/LivePreflight';
import { LiveChecklistSummary } from './components/LiveChecklistSummary';
import { RecoveryConsolePanel } from './components/RecoveryConsolePanel';
import { ReadinessPanel } from './components/ReadinessPanel';
import { SessionLogPanel } from './components/SessionLogPanel';
import { BuildInfoPanel } from './components/BuildInfoPanel';

type LiveStatus = {
  twitch: TwitchStatus | null;
  obs: OBSStatus | null;
  readiness: ReadinessReport | null;
  history: EventHistoryResponse | null;
  loading: boolean;
  error: string;
  readinessError: string;
  historyError: string;
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
    loading: false,
    error: '',
    readinessError: '',
    historyError: '',
  });

  useEffect(() => {
    let cancelled = false;

    async function load() {
      setStatus((current) => ({ ...current, loading: true, error: '', readinessError: '', historyError: '' }));

      const [twitchResult, obsResult, readinessResult, historyResult] = await Promise.allSettled([
        fetchTwitchStatus(apiBaseUrl),
        fetchOBSStatus(apiBaseUrl),
        fetchReadiness(apiBaseUrl),
        fetchEventHistory(24, apiBaseUrl),
      ]);
      if (cancelled) return;

      setStatus({
        twitch: twitchResult.status === 'fulfilled' ? twitchResult.value : null,
        obs: obsResult.status === 'fulfilled' ? obsResult.value : null,
        readiness: readinessResult.status === 'fulfilled' ? readinessResult.value : null,
        history: historyResult.status === 'fulfilled' ? historyResult.value : null,
        loading: false,
        error: twitchResult.status === 'rejected' || obsResult.status === 'rejected' ? 'Some live status checks are unavailable.' : '',
        readinessError: readinessResult.status === 'rejected' ? 'Readiness check unavailable.' : '',
        historyError: historyResult.status === 'rejected' ? 'Event history unavailable.' : '',
      });
    }

    void load();
    return () => {
      cancelled = true;
    };
  }, [apiBaseUrl]);

  return (
    <DashboardShell activePath="/dashboard/live">
      <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
        <p className="hud-label">Live</p>
        <h2 className="hud-section-title">Live ops cockpit</h2>
        <p className="hud-copy mt-1 text-xs text-zinc-300">Use this before going live to catch missing auth, EventSub, OBS, operator, or setup steps.</p>
        <div className="mt-3 flex flex-wrap gap-2">
          <span className="hud-chip hud-chip--square">{status.loading ? 'checking' : 'checked'}</span>
          {status.error ? <span className="hud-chip hud-chip--square hud-accent-amber">{status.error}</span> : null}
        </div>
      </section>

      <RecoveryConsolePanel />
      <BuildInfoPanel apiBaseUrl={apiBaseUrl} />
      <ReadinessPanel report={status.readiness} loading={status.loading} error={status.readinessError} />
      <LivePreflight twitch={status.twitch} obs={status.obs} config={config} operatorSecret={operatorSecret} />
      <EventSubDiagnostics status={status.twitch} />
      <LiveChecklistSummary items={config.setupChecklist} />
      <SessionLogPanel entries={sessionLog} />
      <EventHistoryPanel events={status.history?.events ?? []} loading={status.loading} error={status.historyError} />
    </DashboardShell>
  );
}
