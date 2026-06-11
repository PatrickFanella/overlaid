import { useState } from 'react';
import { loadDashboardConfig, saveDashboardConfig, type DashboardConfig } from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { DashboardShell } from './DashboardShell';
import { CustomCSSPanel } from './components/CustomCSSPanel';
import { OverlaySourceCards } from './components/OverlaySourceCards';
import { StreamPresetEditor } from './components/StreamPresetEditor';

export function DashboardSourcesPage() {
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
    }
    setCopyStatus(`${label} copied`);
  }

  return (
    <DashboardShell activePath="/dashboard/sources">
      <div className="hud-route-grid hud-route-grid--sources">
        <OverlaySourceCards apiBaseUrl={apiBaseUrl} onCopy={copy} />
        <CustomCSSPanel config={config} onChange={updateConfig} onCopy={copy} />
        <StreamPresetEditor config={config} onChange={updateConfig} />
      </div>
      <span className="sr-only" aria-live="polite">{copyStatus}</span>
    </DashboardShell>
  );
}
