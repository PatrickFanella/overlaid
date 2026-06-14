import { DashboardShell } from './DashboardShell';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { loadDashboardConfig, loadOperatorSecret } from '../../lib/dashboardConfig';
import { BuildInfoPanel } from './components/BuildInfoPanel';
import { OpsRouteGuide } from './components/OpsRouteGuide';
import { SystemStatusRail } from './components/SystemStatusRail';

export function DashboardOverviewPage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const config = loadDashboardConfig();
  const operatorSecret = loadOperatorSecret();

  return (
    <DashboardShell activePath="/dashboard">
      <SystemStatusRail apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} config={config} />

      <BuildInfoPanel apiBaseUrl={apiBaseUrl} />

      <OpsRouteGuide />

      <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
        <div className="grid gap-2 md:grid-cols-4">
          <a className="hud-action-button hud-action-button--compact" href="/dashboard/live">Live preflight</a>
          <a className="hud-action-button hud-action-button--compact" href="/dashboard/setup">Setup checklist</a>
          <a className="hud-action-button hud-action-button--compact" href="/dashboard/sources">Source URLs</a>
          <a className="hud-action-button hud-action-button--compact" href="/dashboard/control">Stream control</a>
          <a className="hud-action-button hud-action-button--compact" href="/dashboard/config">Config</a>
        </div>
      </section>
    </DashboardShell>
  );
}
