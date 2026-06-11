import { useState } from 'react';
import { loadDashboardConfig, saveDashboardConfig, type DashboardConfig } from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { DashboardShell } from './DashboardShell';
import { OverlaySourceCards } from './components/OverlaySourceCards';
import { SetupChecklist } from './components/SetupChecklist';
import { StreamPresetEditor } from './components/StreamPresetEditor';

type DashboardSetupPageProps = {
  activePath?: string;
};

export function DashboardSetupPage({ activePath = '/dashboard/setup' }: DashboardSetupPageProps = {}) {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const [config, setConfig] = useState<DashboardConfig>(() => loadDashboardConfig());
  const [copyStatus, setCopyStatus] = useState('');

  function updateConfig(next: DashboardConfig) {
    setConfig(next);
    saveDashboardConfig(next);
  }

  function copy(label: string, value: string) {
    const clipboard = typeof navigator !== 'undefined' ? navigator.clipboard : undefined;

    if (clipboard?.writeText) {
      void clipboard.writeText(value).catch(() => {});
      setCopyStatus(`${label} copied to clipboard`);
      return;
    }

    setCopyStatus(`${label} copied`);
  }

  return (
    <DashboardShell activePath={activePath}>
      <SetupChecklist config={config} onChange={updateConfig} />
      <OverlaySourceCards apiBaseUrl={apiBaseUrl} onCopy={copy} />
      <StreamPresetEditor config={config} onChange={updateConfig} />
      <span className="sr-only" aria-live="polite">{copyStatus}</span>
    </DashboardShell>
  );
}
