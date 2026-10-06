import { useState } from 'react';
import { getActiveAutomationProfile, selectStreamPresetsForProfile } from '../../lib/automationProfiles';
import { loadDashboardConfig, saveDashboardConfig, type DashboardConfig } from '../../lib/dashboardConfig';
import { ActiveProfileTag, ProfileWarnings } from './ActiveProfile';
import { CustomCSSPanel } from './components/CustomCSSPanel';
import { OverlayGallery } from './components/OverlayGallery';
import { SetupChecklist } from './components/SetupChecklist';
import { StreamPresetEditor } from './components/StreamPresetEditor';
import { DashboardShell } from './DashboardShell';
import { useDeskStatus } from './deskStatus';
import { PageHead } from './ui';

export function DashboardOverlaysPage() {
  return (
    <DashboardShell activePath="/dashboard/overlays">
      <Overlays />
    </DashboardShell>
  );
}

function Overlays() {
  const { apiBaseUrl } = useDeskStatus();
  const [config, setConfig] = useState<DashboardConfig>(() => loadDashboardConfig());
  const [copyStatus, setCopyStatus] = useState('');
  const profile = getActiveAutomationProfile(config);
  const { presets, warnings } = selectStreamPresetsForProfile(config);

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
    <>
      <PageHead
        title="Overlays"
        lede="The SUBCULT on-air set. Full-frame scenes take query parameters for their copy; the overlay docs list them."
        actions={
          <>
            {copyStatus ? <span className="desk-tag desk-tag--ok">{copyStatus}</span> : null}
            <ActiveProfileTag profile={profile} />
          </>
        }
      />
      <span className="sr-only" aria-live="polite">
        {copyStatus}
      </span>
      <OverlayGallery apiBaseUrl={apiBaseUrl} onCopy={copy} />
      <div className="grid items-start gap-4 xl:grid-cols-2">
        <SetupChecklist config={config} onChange={updateConfig} />
        <div className="grid gap-4">
          <ProfileWarnings warnings={warnings} />
          <StreamPresetEditor config={config} presets={presets} onChange={updateConfig} />
          <CustomCSSPanel config={config} onChange={updateConfig} onCopy={copy} />
        </div>
      </div>
    </>
  );
}
