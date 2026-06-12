import { useEffect, useState } from 'react';
import { fetchOBSStatus, fetchTwitchStatus, type OBSStatus, type TwitchStatus } from '../../lib/api';
import { loadDashboardConfig, loadOperatorSecret } from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { DashboardShell } from './DashboardShell';
import { EventSubDiagnostics } from './components/EventSubDiagnostics';
import { LivePreflight } from './components/LivePreflight';

type LiveStatus = {
  twitch: TwitchStatus | null;
  obs: OBSStatus | null;
  loading: boolean;
  error: string;
};

export function DashboardLivePage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const config = loadDashboardConfig();
  const operatorSecret = loadOperatorSecret();
  const [status, setStatus] = useState<LiveStatus>({ twitch: null, obs: null, loading: true, error: '' });

  useEffect(() => {
    let cancelled = false;

    async function load() {
      const [twitchResult, obsResult] = await Promise.allSettled([fetchTwitchStatus(apiBaseUrl), fetchOBSStatus(apiBaseUrl)]);
      if (cancelled) return;

      setStatus({
        twitch: twitchResult.status === 'fulfilled' ? twitchResult.value : null,
        obs: obsResult.status === 'fulfilled' ? obsResult.value : null,
        loading: false,
        error: twitchResult.status === 'rejected' || obsResult.status === 'rejected' ? 'Some live status checks are unavailable.' : '',
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
        <h2 className="hud-section-title">Stream readiness</h2>
        <p className="hud-copy mt-1 text-xs text-zinc-300">Use this before going live to catch missing auth, EventSub, OBS, operator, or setup steps.</p>
        <div className="mt-3 flex flex-wrap gap-2">
          <span className="hud-chip hud-chip--square">{status.loading ? 'checking' : 'checked'}</span>
          {status.error ? <span className="hud-chip hud-chip--square hud-accent-amber">{status.error}</span> : null}
        </div>
      </section>

      <LivePreflight twitch={status.twitch} obs={status.obs} config={config} operatorSecret={operatorSecret} />
      <EventSubDiagnostics status={status.twitch} />
    </DashboardShell>
  );
}
