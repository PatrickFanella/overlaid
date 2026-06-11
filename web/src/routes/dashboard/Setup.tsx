import { useState } from 'react';
import { loadDashboardConfig, saveDashboardConfig, type DashboardConfig } from '../../lib/dashboardConfig';
import { DashboardShell } from './DashboardShell';
import { SetupChecklist } from './components/SetupChecklist';

export function DashboardSetupPage() {
  const [config, setConfig] = useState<DashboardConfig>(() => loadDashboardConfig());

  function updateConfig(next: DashboardConfig) {
    setConfig(next);
    saveDashboardConfig(next);
  }

  return (
    <DashboardShell activePath="/dashboard/setup">
      <SetupChecklist config={config} onChange={updateConfig} />
    </DashboardShell>
  );
}
