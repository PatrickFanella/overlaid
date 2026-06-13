import { useState } from 'react';
import { exportDashboardConfig, importDashboardConfig, selectQuickActionsForProfile, selectStreamPresetsForProfile } from '../../../lib/automationProfiles';
import type { AutomationProfile, DashboardConfig } from '../../../lib/dashboardConfig';

type AutomationProfileManagerProps = {
  config: DashboardConfig;
  onChange: (config: DashboardConfig) => void;
};

function parseLines(value: string) {
  return value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

function updateProfile(config: DashboardConfig, profileId: string, patch: Partial<AutomationProfile>) {
  return {
    ...config,
    automationProfiles: config.automationProfiles.map((profile) => (profile.id === profileId ? { ...profile, ...patch } : profile)),
  };
}

export function AutomationProfileManager({ config, onChange }: AutomationProfileManagerProps) {
  const [importRaw, setImportRaw] = useState('');
  const [importError, setImportError] = useState('');
  const activeProfileId = config.automationProfiles.some((profile) => profile.id === config.activeAutomationProfileId)
    ? config.activeAutomationProfileId
    : config.automationProfiles[0]?.id ?? '';
  const activeConfig = config.activeAutomationProfileId === activeProfileId ? config : { ...config, activeAutomationProfileId: activeProfileId };
  const quickActionSelection = selectQuickActionsForProfile(activeConfig);
  const streamPresetSelection = selectStreamPresetsForProfile(activeConfig);

  function setActiveProfileId(profileId: string) {
    onChange({ ...config, activeAutomationProfileId: profileId });
  }

  function handleImport() {
    try {
      JSON.parse(importRaw);
      onChange(importDashboardConfig(importRaw));
      setImportError('');
    } catch {
      setImportError('Import failed: invalid dashboard config JSON.');
    }
  }

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <p className="hud-label">Automation profiles</p>
          <h2 className="hud-section-title">Automation profiles</h2>
          <p className="hud-copy mt-1 text-xs text-zinc-300">Choose the active profile, edit profile bundles, and import or export the dashboard config locally.</p>
        </div>
      </div>

      <div className="mt-3 grid gap-3 xl:grid-cols-2">
        <div className="hud-panel hud-corners hud-panel--flat hud-compact-card">
          <label className="grid gap-2 text-xs text-zinc-300">
            <span className="hud-kicker">Active profile</span>
            <select className="hud-input" value={activeProfileId} onChange={(event) => setActiveProfileId(event.target.value)}>
              {config.automationProfiles.map((profile) => (
                <option key={profile.id} value={profile.id}>
                  {profile.title}
                </option>
              ))}
            </select>
          </label>

          <div className="mt-3 grid gap-2 text-xs text-zinc-300">
            <div>
              <p className="hud-kicker">Quick action warnings</p>
              {quickActionSelection.warnings.length > 0 ? (
                <ul className="mt-2 grid gap-1 rounded-2xl border border-amber-400/30 bg-amber-400/10 px-4 py-3 text-amber-100">
                  {quickActionSelection.warnings.map((warning) => (
                    <li key={`${warning.profileId}-${warning.message}`}>{warning.message}</li>
                  ))}
                </ul>
              ) : (
                <p className="mt-2 text-zinc-400">No quick action warnings for the active profile.</p>
              )}
            </div>

            <div>
              <p className="hud-kicker">Stream preset warnings</p>
              {streamPresetSelection.warnings.length > 0 ? (
                <ul className="mt-2 grid gap-1 rounded-2xl border border-amber-400/30 bg-amber-400/10 px-4 py-3 text-amber-100">
                  {streamPresetSelection.warnings.map((warning) => (
                    <li key={`${warning.profileId}-${warning.message}`}>{warning.message}</li>
                  ))}
                </ul>
              ) : (
                <p className="mt-2 text-zinc-400">No stream preset warnings for the active profile.</p>
              )}
            </div>
          </div>
        </div>

        <div className="hud-panel hud-corners hud-panel--flat hud-compact-card">
          <p className="hud-label">Export dashboard config</p>
          <p className="mt-1 text-xs text-zinc-300">operator secret is not exported</p>
          <textarea className="hud-input mt-3 min-h-48 font-mono text-xs" value={exportDashboardConfig(config)} readOnly />

          <label className="mt-3 grid gap-2 text-xs text-zinc-300">
            <span className="hud-kicker">Import dashboard config</span>
            <textarea className="hud-input min-h-32 font-mono text-xs" value={importRaw} onChange={(event) => setImportRaw(event.target.value)} placeholder="Paste exported dashboard config JSON here" />
          </label>
          <div className="mt-3 flex flex-wrap items-center gap-3">
            <button className="hud-chip" type="button" onClick={handleImport}>
              Import dashboard config
            </button>
            {importError ? <p className="text-xs text-amber-200">{importError}</p> : null}
          </div>
        </div>
      </div>

      <div className="mt-3 grid gap-3">
        {config.automationProfiles.map((profile) => {
          const isActive = profile.id === activeProfileId;

          return (
            <article key={profile.id} className={`hud-panel hud-corners hud-panel--flat hud-compact-card ${isActive ? 'hud-accent-cyan border-cyan-200/50' : ''}`}>
              <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
                <div>
                  <p className="hud-kicker">Profile {profile.id}</p>
                  <p className="mt-1 text-sm font-bold text-white">{profile.title}</p>
                </div>
                {isActive ? <span className="hud-badge self-start">active</span> : null}
              </div>

              <div className="mt-3 grid gap-3 lg:grid-cols-2">
                <label className="grid gap-2 text-xs text-zinc-300">
                  <span className="hud-kicker">Title</span>
                  <input className="hud-input" type="text" value={profile.title} onChange={(event) => onChange(updateProfile(config, profile.id, { title: event.target.value }))} />
                </label>

                <label className="grid gap-2 text-xs text-zinc-300">
                  <span className="hud-kicker">Description</span>
                  <input className="hud-input" type="text" value={profile.description} onChange={(event) => onChange(updateProfile(config, profile.id, { description: event.target.value }))} />
                </label>

                <label className="grid gap-2 text-xs text-zinc-300 lg:col-span-2">
                  <span className="hud-kicker">Quick action IDs</span>
                  <textarea
                    className="hud-input min-h-24 font-mono text-xs"
                    value={profile.quickActionIds.join('\n')}
                    onChange={(event) => onChange(updateProfile(config, profile.id, { quickActionIds: parseLines(event.target.value) }))}
                  />
                </label>

                <label className="grid gap-2 text-xs text-zinc-300 lg:col-span-2">
                  <span className="hud-kicker">Stream preset IDs</span>
                  <textarea
                    className="hud-input min-h-24 font-mono text-xs"
                    value={profile.streamPresetIds.join('\n')}
                    onChange={(event) => onChange(updateProfile(config, profile.id, { streamPresetIds: parseLines(event.target.value) }))}
                  />
                </label>
              </div>
            </article>
          );
        })}
      </div>
    </section>
  );
}
