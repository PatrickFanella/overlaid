import { useEffect, useState } from 'react';
import { fetchEventHistory, type EventHistoryResponse } from '../../lib/api';
import { getActiveAutomationProfile, selectQuickActionsForProfile } from '../../lib/automationProfiles';
import { loadDashboardConfig } from '../../lib/dashboardConfig';
import { ActiveProfileTag, ProfileWarnings } from './ActiveProfile';
import { BRBControl } from './components/BRBControl';
import { EventHistoryPanel } from './components/EventHistoryPanel';
import { LivePreflight } from './components/LivePreflight';
import { QuickActionRunner } from './components/QuickActionRunner';
import { SessionLogPanel } from './components/SessionLogPanel';
import { StreamSessionPanel } from './components/StreamSessionPanel';
import { DashboardShell } from './DashboardShell';
import { useDeskStatus } from './deskStatus';
import { PageHead, Panel } from './ui';
import { useOperatorActions } from './useOperatorActions';

export function DashboardOnAirPage() {
  return (
    <DashboardShell activePath="/dashboard">
      <OnAir />
    </DashboardShell>
  );
}

function OnAir() {
  const status = useDeskStatus();
  const [config] = useState(() => loadDashboardConfig());
  const { sessionLog, runAction, isRunning } = useOperatorActions();
  const profile = getActiveAutomationProfile(config);
  const { actions, warnings } = selectQuickActionsForProfile(config);
  const history = useRecentEvents(status.apiBaseUrl, status.checkedAt, 24, status.operatorSecret);

  return (
    <>
      <PageHead title="On air" lede="Everything you touch during a stream. Setup and editing live on the other pages." actions={<ActiveProfileTag profile={profile} />} />

      <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_400px]">
        <div className="grid gap-4">
          <Panel
            kicker="Quick actions"
            title="Run configured actions"
            description={profile.description}
            actions={
              <a className="desk-btn desk-btn--small desk-btn--ghost" href="/dashboard/scenes#quick-action-presets">
                Edit presets
              </a>
            }
          >
            <ProfileWarnings warnings={warnings} />
            <QuickActionRunner bare actions={actions} actionCatalog={config.quickActions} apiBaseUrl={status.apiBaseUrl} operatorSecret={status.operatorSecret} isRunning={isRunning} runAction={runAction} />
          </Panel>
          <BRBControl compact apiBaseUrl={status.apiBaseUrl} operatorSecret={status.operatorSecret} isRunning={isRunning} runAction={runAction} />
          <SessionLogPanel entries={sessionLog} limit={6} />
        </div>

        <div className="grid gap-4">
          <StreamSessionPanel compact apiBaseUrl={status.apiBaseUrl} operatorSecret={status.operatorSecret} status={status.session} loading={status.loading} error={status.errors.session} onChange={status.setSession} />
          <LivePreflight twitch={status.twitch} obs={status.obs} config={config} operatorSecret={status.operatorSecret} />
          <EventHistoryPanel title="Latest events" events={history.data?.events ?? []} loading={history.loading} error={history.error} limit={8} />
        </div>
      </div>
    </>
  );
}

// Reloads alongside the top bar's Recheck button.
export function useRecentEvents(apiBaseUrl: string, refreshKey: string, limit = 24, operatorSecret?: string) {
  const [state, setState] = useState<{ data: EventHistoryResponse | null; loading: boolean; error: string }>({ data: null, loading: true, error: '' });

  useEffect(() => {
    let cancelled = false;
    fetchEventHistory(limit, apiBaseUrl, operatorSecret)
      .then((data) => {
        if (!cancelled) setState({ data, loading: false, error: '' });
      })
      .catch(() => {
        if (!cancelled) setState({ data: null, loading: false, error: 'Event history unavailable.' });
      });
    return () => {
      cancelled = true;
    };
  }, [apiBaseUrl, refreshKey, limit, operatorSecret]);

  return state;
}
