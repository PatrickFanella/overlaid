import { useState } from 'react';
import { exportDashboardConfig, importDashboardConfig, selectQuickActionsForProfile, selectStreamPresetsForProfile } from '../../../lib/automationProfiles';
import type { AutomationProfile, DashboardConfig } from '../../../lib/dashboardConfig';
import { Empty, Field, Notice, Panel, SubPanel, Tag } from '../ui';

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
    <Panel
      kicker="Automation"
      title="Automation profiles"
      description="Choose the active profile, edit profile bundles, and import or export the dashboard config locally."
    >
      <div className="grid gap-4 xl:grid-cols-2">
        <SubPanel title="Active profile">
          <Field label="Active profile">
            <select className="desk-input" value={activeProfileId} onChange={(event) => setActiveProfileId(event.target.value)}>
              {config.automationProfiles.map((profile) => (
                <option key={profile.id} value={profile.id}>
                  {profile.title}
                </option>
              ))}
            </select>
          </Field>

          <div className="grid gap-2">
            <p className="desk-sub__title">Quick action warnings</p>
            {quickActionSelection.warnings.length > 0 ? (
              <Notice tone="warn">
                <ul>
                  {quickActionSelection.warnings.map((warning) => (
                    <li key={`${warning.profileId}-${warning.message}`}>{warning.message}</li>
                  ))}
                </ul>
              </Notice>
            ) : (
              <Empty>No quick action warnings for the active profile.</Empty>
            )}
          </div>

          <div className="grid gap-2">
            <p className="desk-sub__title">Stream preset warnings</p>
            {streamPresetSelection.warnings.length > 0 ? (
              <Notice tone="warn">
                <ul>
                  {streamPresetSelection.warnings.map((warning) => (
                    <li key={`${warning.profileId}-${warning.message}`}>{warning.message}</li>
                  ))}
                </ul>
              </Notice>
            ) : (
              <Empty>No stream preset warnings for the active profile.</Empty>
            )}
          </div>
        </SubPanel>

        <div className="grid gap-4">
          <SubPanel title="Export dashboard config">
            <p className="desk-field__hint">The operator secret is not exported.</p>
            <textarea className="desk-input desk-input--mono min-h-48" value={exportDashboardConfig(config)} readOnly />
          </SubPanel>

          <SubPanel title="Import dashboard config">
            <textarea
              className="desk-input desk-input--mono min-h-32"
              aria-label="Import dashboard config"
              value={importRaw}
              onChange={(event) => setImportRaw(event.target.value)}
              placeholder="Paste exported dashboard config JSON here"
            />
            <div className="flex flex-wrap items-center gap-3">
              <button className="desk-btn" type="button" onClick={handleImport}>
                Import dashboard config
              </button>
            </div>
            {importError ? (
              <Notice tone="bad" role="alert">
                {importError}
              </Notice>
            ) : null}
          </SubPanel>
        </div>
      </div>

      <ul className="desk-rows">
        {config.automationProfiles.map((profile) => {
          const isActive = profile.id === activeProfileId;

          return (
            <li key={profile.id} className={`desk-row ${isActive ? 'desk-row--ok' : ''}`}>
              <div className="desk-row__line">
                <p className="desk-row__title">
                  {profile.title} <span className="desk-row__meta mono">{profile.id}</span>
                </p>
                {isActive ? <Tag tone="ok">active</Tag> : null}
              </div>

              <div className="mt-2 grid gap-3 lg:grid-cols-2">
                <Field label="Title">
                  <input className="desk-input" type="text" value={profile.title} onChange={(event) => onChange(updateProfile(config, profile.id, { title: event.target.value }))} />
                </Field>

                <Field label="Description">
                  <input className="desk-input" type="text" value={profile.description} onChange={(event) => onChange(updateProfile(config, profile.id, { description: event.target.value }))} />
                </Field>

                <Field label="Quick action IDs" hint="One per line." className="lg:col-span-2">
                  <textarea
                    className="desk-input desk-input--mono"
                    value={profile.quickActionIds.join('\n')}
                    onChange={(event) => onChange(updateProfile(config, profile.id, { quickActionIds: parseLines(event.target.value) }))}
                  />
                </Field>

                <Field label="Stream preset IDs" hint="One per line." className="lg:col-span-2">
                  <textarea
                    className="desk-input desk-input--mono"
                    value={profile.streamPresetIds.join('\n')}
                    onChange={(event) => onChange(updateProfile(config, profile.id, { streamPresetIds: parseLines(event.target.value) }))}
                  />
                </Field>
              </div>
            </li>
          );
        })}
      </ul>
    </Panel>
  );
}
