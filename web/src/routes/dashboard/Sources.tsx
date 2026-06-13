import { useState } from 'react';
import { loadDashboardConfig, saveDashboardConfig, type DashboardConfig } from '../../lib/dashboardConfig';
import { getActiveAutomationProfile, selectStreamPresetsForProfile } from '../../lib/automationProfiles';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { DashboardShell } from './DashboardShell';
import { CustomCSSPanel } from './components/CustomCSSPanel';
import { OverlaySourceCards } from './components/OverlaySourceCards';
import { StreamPresetEditor } from './components/StreamPresetEditor';

export function DashboardSourcesPage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const [config, setConfig] = useState<DashboardConfig>(() => loadDashboardConfig());
  const [copyStatus, setCopyStatus] = useState('');
  const activeAutomationProfile = getActiveAutomationProfile(config);
  const { presets: profilePresets, warnings: profileWarnings } = selectStreamPresetsForProfile(config);

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
      <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
        <p className="hud-label">Active profile</p>
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <span className="hud-chip hud-chip--square hud-accent-cyan">{activeAutomationProfile.title}</span>
          <span className="text-xs text-zinc-400">{activeAutomationProfile.description}</span>
        </div>
        {profileWarnings.length > 0 ? (
          <ul className="mt-3 grid gap-1 rounded-2xl border border-amber-400/30 bg-amber-400/10 px-3 py-2 text-xs text-amber-100">
            {profileWarnings.map((warning) => (
              <li key={`${warning.profileId}:${warning.message}`}>{warning.message}</li>
            ))}
          </ul>
        ) : null}
      </section>
      <div className="hud-route-grid hud-route-grid--sources">
        <OverlaySourceCards apiBaseUrl={apiBaseUrl} onCopy={copy} />
        <CustomCSSPanel config={config} onChange={updateConfig} onCopy={copy} />
        <StreamPresetEditor config={config} presets={profilePresets} onChange={updateConfig} />
      </div>
      <span className="sr-only" aria-live="polite">{copyStatus}</span>
    </DashboardShell>
  );
}
