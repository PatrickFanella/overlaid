import { useEffect, useState } from 'react';
import { fetchDashboardConfig, fetchOBSInputs, fetchOBSScenes, saveSharedDashboardConfig, type OBSInput } from '../../lib/api';
import {
  loadDashboardConfig,
  loadOperatorSecret,
  saveDashboardConfig,
  saveOperatorSecret,
  type DashboardConfig,
  normalizeDashboardConfig,
  type QuickAction,
} from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { validateQuickActionsAgainstOBS, type OBSValidationIssue } from '../../lib/obsValidation';
import { DashboardShell } from './DashboardShell';
import { AutomationProfileManager } from './components/AutomationProfileManager';
import { CustomizablePanelLayout } from './components/CustomizablePanelLayout';
import type { PanelLayoutItem } from '../../lib/dashboardLayout';
import { Field, Notice, PageHead, Panel, SubPanel, Tag } from './ui';

export function DashboardSettingsPage() {
  return (
    <DashboardShell activePath="/dashboard/settings">
      <Settings />
    </DashboardShell>
  );
}

function Settings() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const [operatorSecret, setOperatorSecret] = useState('');
  const [webhookSecret, setWebhookSecret] = useState('');
  const [config, setConfig] = useState<DashboardConfig>(() => loadDashboardConfig());
  const [sharedConfigStatus, setSharedConfigStatus] = useState('');
  const [obsImportStatus, setOBSImportStatus] = useState('');
  const [obsSceneNames, setOBSSceneNames] = useState<string[]>([]);
  const [obsInputNames, setOBSInputNames] = useState<string[]>([]);
  const [obsBrowserInputNames, setOBSBrowserInputNames] = useState<string[]>([]);
  const [obsAssignments, setOBSAssignments] = useState<OBSAssignments>({ mainScene: '', brbScene: '', alertsSource: '' });
  const [obsValidationIssues, setOBSValidationIssues] = useState<OBSValidationIssue[]>([]);

  useEffect(() => {
    setOperatorSecret(loadOperatorSecret());
  }, []);

  function updateOperatorSecret(value: string) {
    setOperatorSecret(value);
    saveOperatorSecret(value);
  }

  function updateConfig(next: DashboardConfig) {
    setConfig(next);
    saveDashboardConfig(next);
  }

  function updateConfigPanelLayout(items: PanelLayoutItem[]) {
    setConfig((current) => {
      const next = { ...current, panelLayouts: { ...current.panelLayouts, config: items } };
      saveDashboardConfig(next);
      return next;
    });
  }

  async function loadSharedConfig() {
    setSharedConfigStatus('Loading shared config…');
    try {
      const response = await fetchDashboardConfig(apiBaseUrl, operatorSecret);
      if (response.loadError) {
        setSharedConfigStatus(`Shared config unavailable: ${response.loadError}`);
        return;
      }
      if (!response.config) {
        setSharedConfigStatus('No shared config saved yet.');
        return;
      }
      const next = normalizeDashboardConfig(response.config);
      updateConfig(next);
      setSharedConfigStatus(response.updatedAt ? `Loaded shared config from ${new Date(response.updatedAt).toLocaleString()}.` : 'Loaded shared config.');
    } catch (error) {
      setSharedConfigStatus(error instanceof Error ? error.message : 'Failed to load shared config.');
    }
  }

  async function saveSharedConfig() {
    setSharedConfigStatus('Saving shared config…');
    try {
      const response = await saveSharedDashboardConfig(config, apiBaseUrl, operatorSecret);
      setSharedConfigStatus(response.updatedAt ? `Saved shared config at ${new Date(response.updatedAt).toLocaleString()}.` : 'Saved shared config.');
    } catch (error) {
      setSharedConfigStatus(error instanceof Error ? error.message : 'Failed to save shared config.');
    }
  }

  async function loadOBSAssignments() {
    setOBSImportStatus('Loading OBS scenes and sources…');
    try {
      const [scenes, inputs] = await Promise.all([fetchOBSScenes(apiBaseUrl, operatorSecret), fetchOBSInputs(apiBaseUrl, operatorSecret)]);
      const sceneNames = scenes.scenes.map((scene) => scene.sceneName);
      const inputNames = inputs.map((input) => input.inputName);
      const browserInputNames = inputs.filter((input) => isBrowserInput(input)).map((input) => input.inputName);
      const nextAssignments = suggestOBSAssignments(sceneNames, browserInputNames, config);
      setOBSSceneNames(sceneNames);
      setOBSInputNames(inputNames);
      setOBSBrowserInputNames(browserInputNames);
      setOBSAssignments(nextAssignments);
      setOBSValidationIssues(validateQuickActionsAgainstOBS(config.quickActions, sceneNames, inputNames));
      setOBSImportStatus('OBS names loaded. Review assignments, then apply.');
    } catch (error) {
      setOBSImportStatus(error instanceof Error ? error.message : 'Failed to load OBS names.');
    }
  }

  function applyOBSAssignments() {
    const next = applyOBSAssignmentsToConfig(config, obsAssignments);
    updateConfig(next);
    setOBSValidationIssues(validateQuickActionsAgainstOBS(next.quickActions, obsSceneNames, obsInputNames));
    setOBSImportStatus('Applied OBS assignments to quick actions. Use Save shared if you want other browsers to pull them.');
  }

  function validateOBSAssignments() {
    if (obsSceneNames.length === 0 && obsInputNames.length === 0) {
      setOBSImportStatus('Load from OBS before validating quick actions.');
      return;
    }
    const issues = validateQuickActionsAgainstOBS(config.quickActions, obsSceneNames, obsInputNames);
    setOBSValidationIssues(issues);
    setOBSImportStatus(issues.length === 0 ? 'All OBS quick-action scene/source names are valid.' : `${issues.length} OBS assignment issue(s) found.`);
  }

  function setMprisDecision(mprisDecision: DashboardConfig['mprisDecision']) {
    const next = { ...config, mprisDecision };
    updateConfig(next);
  }

  function updateDisplay(key: keyof DashboardConfig['display'], value: number) {
    const next = { ...config, display: { ...config.display, [key]: Math.max(0, Math.floor(value || 0)) } };
    updateConfig(next);
  }

  const obsLoaded = obsSceneNames.length > 0 || obsInputNames.length > 0;

  const safeConfigPanels = [
    {
      id: 'dashboard-config',
      node: (
        <Panel
          kicker="Shared dashboard config"
          title="Sync between browsers"
          description="Push or pull non-secret dashboard preferences through the API. Operator and webhook secrets never leave this browser."
          actions={
            <>
              <button className="desk-btn desk-btn--small" type="button" onClick={() => void loadSharedConfig()}>
                Load shared
              </button>
              <button className="desk-btn desk-btn--small desk-btn--primary" type="button" onClick={() => void saveSharedConfig()}>
                Save shared
              </button>
            </>
          }
        >
          {sharedConfigStatus ? <p className="desk-row__text" role="status">{sharedConfigStatus}</p> : <p className="desk-empty">Nothing loaded or saved this session.</p>}
        </Panel>
      ),
    },
    {
      id: 'obs-import',
      node: (
        <Panel
          kicker="OBS import"
          title="Map OBS names to quick actions"
          description="Pull live scene and source names from OBS into the built-in quick actions. Assign them by hand where auto-match misses."
          actions={
            <>
              <Tag tone={obsValidationIssues.length > 0 ? 'bad' : obsLoaded ? 'ok' : 'neutral'}>{obsLoaded ? `${obsValidationIssues.length} OBS issues` : 'OBS not loaded'}</Tag>
              <Tag>{obsSceneNames.length} scenes</Tag>
              <Tag>{obsInputNames.length} inputs</Tag>
            </>
          }
        >
          <div className="flex flex-wrap gap-2">
            <button className="desk-btn" type="button" onClick={() => void loadOBSAssignments()}>
              Load from OBS
            </button>
            <button className="desk-btn desk-btn--primary" type="button" disabled={!obsAssignments.mainScene && !obsAssignments.brbScene && !obsAssignments.alertsSource} onClick={applyOBSAssignments}>
              Apply assignments
            </button>
            <button className="desk-btn" type="button" onClick={validateOBSAssignments}>
              Validate quick actions
            </button>
          </div>
          <div className="grid gap-3 md:grid-cols-3">
            <AssignmentSelect label="Main scene" value={obsAssignments.mainScene} options={obsSceneNames} onChange={(mainScene) => setOBSAssignments((current) => ({ ...current, mainScene }))} />
            <AssignmentSelect label="BRB / standby scene" value={obsAssignments.brbScene} options={obsSceneNames} onChange={(brbScene) => setOBSAssignments((current) => ({ ...current, brbScene }))} />
            <AssignmentSelect label="Alerts browser source" value={obsAssignments.alertsSource} options={obsBrowserInputNames} onChange={(alertsSource) => setOBSAssignments((current) => ({ ...current, alertsSource }))} />
          </div>
          {obsValidationIssues.length > 0 ? (
            <Notice tone="bad">
              <ul>
                {obsValidationIssues.slice(0, 8).map((issue) => (
                  <li key={`${issue.actionId}:${issue.field}:${issue.value}`}>
                    {issue.label}: {issue.message}
                  </li>
                ))}
              </ul>
            </Notice>
          ) : null}
          {obsImportStatus ? <p className="desk-row__text" role="status">{obsImportStatus}</p> : null}
        </Panel>
      ),
    },
    {
      id: 'overlay-noise',
      node: (
        <Panel kicker="Overlay limits" title="Overlay noise controls" description="How many items stay on screen, and how long a repeated alert is suppressed. Overlays read these when they load.">
          <div className="grid gap-3 sm:grid-cols-2">
            <NumberControl label="Alert max visible" value={config.display.alertMaxVisible} onChange={(value) => updateDisplay('alertMaxVisible', value)} />
            <NumberControl label="Alert dedupe seconds" value={config.display.alertDedupeSeconds} onChange={(value) => updateDisplay('alertDedupeSeconds', value)} />
            <NumberControl label="Chat max visible" value={config.display.chatMaxVisible} onChange={(value) => updateDisplay('chatMaxVisible', value)} />
            <NumberControl label="Ticker max items" value={config.display.tickerMaxItems} onChange={(value) => updateDisplay('tickerMaxItems', value)} />
          </div>
        </Panel>
      ),
    },
    {
      id: 'automation-profiles',
      node: (
        <div id="automation-profiles">
          <AutomationProfileManager config={config} onChange={updateConfig} />
        </div>
      ),
    },
    {
      id: 'mpris',
      node: (
        <Panel kicker="Now playing" title="MPRIS decision" description="Use host MPRIS for the now-playing overlay, keep it off, or decide later.">
          <div className="flex flex-wrap gap-2" role="radiogroup" aria-label="MPRIS decision">
            {(['enabled', 'disabled', 'undecided'] as const).map((value) => (
              <button
                key={value}
                className={`desk-btn capitalize ${config.mprisDecision === value ? 'desk-btn--active' : ''}`}
                type="button"
                role="radio"
                aria-checked={config.mprisDecision === value}
                onClick={() => setMprisDecision(value)}
              >
                {value}
              </button>
            ))}
          </div>
        </Panel>
      ),
    },
  ];

  return (
    <>
      <PageHead
        title="Settings"
        lede="Configure deliberately. Changes save to this browser immediately; use Save shared to send non-secret settings to the API. Secrets stay in browser session state."
        actions={<Tag>API {apiBaseUrl}</Tag>}
      />

      <Panel kicker="Operator access" title="Operator secret" description="Used for protected dashboard actions. Stored for this browser session only, and this is the only page that edits it.">
        <div className="grid gap-4 lg:grid-cols-2">
          <SubPanel>
            <Field label="Secret header" hint={operatorSecret ? 'Operator secret loaded.' : 'No operator secret stored.'}>
              <input className="desk-input" type="password" autoComplete="off" value={operatorSecret} onChange={(event) => updateOperatorSecret(event.target.value)} placeholder="optional unless the API requires it" />
            </Field>
          </SubPanel>
          <SubPanel>
            <Field label="Webhook test secret" hint={webhookSecret ? 'Webhook secret ready. Not persisted.' : 'Optional helper for webhook tests. Not persisted.'}>
              <input className="desk-input" type="password" autoComplete="off" value={webhookSecret} onChange={(event) => setWebhookSecret(event.target.value)} placeholder="optional" />
            </Field>
          </SubPanel>
        </div>
      </Panel>

      <CustomizablePanelLayout
        page="config"
        items={config.panelLayouts.config}
        onChange={updateConfigPanelLayout}
        description="Reorder or hide safe configuration panels. Secret controls stay locked."
        className="grid items-start gap-4 xl:grid-cols-2"
        panels={safeConfigPanels}
      />
    </>
  );
}

type OBSAssignments = {
  mainScene: string;
  brbScene: string;
  alertsSource: string;
};

function AssignmentSelect({ label, value, options, onChange }: { label: string; value: string; options: string[]; onChange: (value: string) => void }) {
  return (
    <Field label={label}>
      <select className="desk-input" value={value} onChange={(event) => onChange(event.target.value)}>
        <option value="">Assign…</option>
        {options.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </select>
    </Field>
  );
}

function isBrowserInput(input: OBSInput) {
  return input.inputKind === 'browser_source' || input.inputName.toLowerCase().includes('alert') || input.inputName.toLowerCase().includes('browser');
}

function suggestOBSAssignments(sceneNames: string[], browserInputNames: string[], config: DashboardConfig): OBSAssignments {
  return {
    mainScene: findPreferred(sceneNames, ['40_MAIN', 'MAIN'], currentActionValue(config, 'main-scene', 'sceneName')),
    brbScene: findPreferred(sceneNames, ['30_STAND_BY', 'STAND_BY', 'BRB'], currentActionValue(config, 'brb-scene', 'sceneName')),
    alertsSource: findPreferred(browserInputNames, ['ALERTS', 'Alerts Browser'], currentActionValue(config, 'refresh-alerts', 'inputName')),
  };
}

function currentActionValue(config: DashboardConfig, id: string, key: 'sceneName' | 'inputName') {
  const action = config.quickActions.find((item) => item.id === id) as (QuickAction & Partial<Record<typeof key, string>>) | undefined;
  return action?.[key] || '';
}

function findPreferred(options: string[], preferred: string[], current: string) {
  if (current && options.includes(current)) return current;
  for (const candidate of preferred) {
    const exact = options.find((option) => option === candidate);
    if (exact) return exact;
    const loose = options.find((option) => option.toLowerCase().includes(candidate.toLowerCase()));
    if (loose) return loose;
  }
  return '';
}

function applyOBSAssignmentsToConfig(config: DashboardConfig, assignments: OBSAssignments): DashboardConfig {
  return {
    ...config,
    quickActions: config.quickActions.map((action) => applyOBSAssignmentToAction(action, assignments)),
  };
}

function applyOBSAssignmentToAction(action: QuickAction, assignments: OBSAssignments): QuickAction {
  if ((action.id === 'main-scene' || action.id === 'preset-main') && (action.kind === 'setScene' || action.kind === 'runPreset') && assignments.mainScene) {
    return { ...action, sceneName: assignments.mainScene };
  }
  if (action.id === 'brb-scene' && action.kind === 'setScene' && assignments.brbScene) {
    return { ...action, sceneName: assignments.brbScene };
  }
  if (action.id === 'refresh-alerts' && action.kind === 'refreshSource' && assignments.alertsSource) {
    return { ...action, inputName: assignments.alertsSource };
  }
  return action;
}

function NumberControl({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) {
  return (
    <Field label={label}>
      <input className="desk-input" type="number" min="0" value={value} onChange={(event) => onChange(Number(event.target.value))} />
    </Field>
  );
}
