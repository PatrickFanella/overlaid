import { useState } from 'react';
import { loadDashboardConfig, saveDashboardConfig } from '../../lib/dashboardConfig';
import type { PanelLayoutItem } from '../../lib/dashboardLayout';
import { BuildInfoPanel } from './components/BuildInfoPanel';
import { CustomizablePanelLayout } from './components/CustomizablePanelLayout';
import { EventSubDiagnostics } from './components/EventSubDiagnostics';
import { LiveChecklistSummary } from './components/LiveChecklistSummary';
import { LivePreflight } from './components/LivePreflight';
import { ReadinessPanel } from './components/ReadinessPanel';
import { RecoveryConsolePanel } from './components/RecoveryConsolePanel';
import { DashboardShell } from './DashboardShell';
import { useDeskStatus } from './deskStatus';
import { PageHead } from './ui';

export function DashboardHealthPage() {
  return (
    <DashboardShell activePath="/dashboard/health">
      <Health />
    </DashboardShell>
  );
}

function Health() {
  const status = useDeskStatus();
  const [config, setConfig] = useState(() => loadDashboardConfig());

  function updateLayout(items: PanelLayoutItem[]) {
    setConfig((current) => {
      const next = { ...current, panelLayouts: { ...current.panelLayouts, live: items } };
      saveDashboardConfig(next);
      return next;
    });
  }

  return (
    <>
      <PageHead
        title="Health"
        lede="Check readiness, recover production issues, and review EventSub before going live."
        actions={
          <button className="desk-btn desk-btn--small" type="button" disabled={status.loading} onClick={status.refresh}>
            {status.loading ? 'Checking…' : 'Run checks again'}
          </button>
        }
      />
      <RecoveryConsolePanel />
      <CustomizablePanelLayout
        page="live"
        items={config.panelLayouts.live}
        onChange={updateLayout}
        className="grid items-start gap-4 lg:grid-cols-2"
        panels={[
          { id: 'readiness', node: <ReadinessPanel report={status.readiness} loading={status.loading} error={status.errors.readiness} /> },
          { id: 'preflight', node: <LivePreflight twitch={status.twitch} obs={status.obs} config={config} operatorSecret={status.operatorSecret} /> },
          { id: 'eventsub', node: <EventSubDiagnostics status={status.twitch} /> },
          { id: 'build-info', node: <BuildInfoPanel apiBaseUrl={status.apiBaseUrl} /> },
          { id: 'checklist', node: <LiveChecklistSummary items={config.setupChecklist} /> },
        ]}
      />
    </>
  );
}
