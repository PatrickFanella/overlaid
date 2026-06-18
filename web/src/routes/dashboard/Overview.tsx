import { DashboardShell } from './DashboardShell';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { loadDashboardConfig, loadOperatorSecret } from '../../lib/dashboardConfig';
import { BuildInfoPanel } from './components/BuildInfoPanel';
import { ConsolePageHeader } from './components/ConsolePageHeader';
import { ConsoleSection } from './components/ConsoleSection';
import { OpsRouteGuide } from './components/OpsRouteGuide';
import { SystemStatusRail } from './components/SystemStatusRail';

export function DashboardOverviewPage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const config = loadDashboardConfig();
  const operatorSecret = loadOperatorSecret();

  return (
    <DashboardShell activePath="/dashboard">
      <ConsolePageHeader
        eyebrow="Mission control"
        title="Broadcast ops console"
        description="Start with live health, jump to operation surfaces, or configure sources before a stream."
        chips={['Monitor first', 'Operate second', 'Configure deliberately']}
      />

      <SystemStatusRail apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} config={config} />

      <BuildInfoPanel apiBaseUrl={apiBaseUrl} />

      <OpsRouteGuide />

      <ConsoleSection eyebrow="Quick launch" title="Route shortcuts" description="Jump directly to the primary dashboard surfaces." accent="fuchsia">
        <div className="grid gap-2 md:grid-cols-4">
          <a className="hud-action-button hud-action-button--compact" href="/dashboard/live">Live preflight</a>
          <a className="hud-action-button hud-action-button--compact" href="/dashboard/setup">Setup checklist</a>
          <a className="hud-action-button hud-action-button--compact" href="/dashboard/sources">Source URLs</a>
          <a className="hud-action-button hud-action-button--compact" href="/dashboard/control">Stream control</a>
          <a className="hud-action-button hud-action-button--compact" href="/dashboard/bot">Bot ops</a>
          <a className="hud-action-button hud-action-button--compact" href="/dashboard/config">Config</a>
        </div>
      </ConsoleSection>
    </DashboardShell>
  );
}
