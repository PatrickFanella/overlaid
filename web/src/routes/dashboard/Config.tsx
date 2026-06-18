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
import { ConsolePageHeader } from './components/ConsolePageHeader';
import { AutomationProfileManager } from './components/AutomationProfileManager';
import { TwitchInteractionsManager } from './components/TwitchInteractionsManager';

export function DashboardConfigPage() {
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

  return (
    <DashboardShell activePath="/dashboard/config">
      <ConsolePageHeader
        eyebrow="Configure deliberately"
        title="Operator config"
        description="Edit local operator preferences, OBS mappings, Twitch interaction rules, and overlay display behavior. Secrets stay in browser session state."
        accent="purple"
        chips={[`API ${apiBaseUrl}`, operatorSecret ? 'operator secret loaded' : 'operator secret missing']}
      />

      <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
        <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
          <div>
            <p className="hud-label">Operator config</p>
            <h2 className="hud-section-title">Config</h2>
            <p className="hud-copy mt-1 text-xs text-zinc-300">Local operator-console preferences. Secrets stay in browser session state.</p>
          </div>
          <span className="hud-chip">API {apiBaseUrl}</span>
        </div>

        <div className="mt-3 grid gap-3 lg:grid-cols-2">
          <div className="hud-panel hud-corners hud-panel--flat hud-accent-fuchsia hud-compact-card">
            <p className="hud-label">Operator secret</p>
            <p className="mt-1 text-xs leading-5 text-zinc-300">Used for protected dashboard actions. Session-only.</p>
            <label className="mt-2 grid gap-1 text-xs text-zinc-300">
              <span className="hud-kicker">Secret header</span>
              <input className="hud-input" type="password" value={operatorSecret} onChange={(event) => updateOperatorSecret(event.target.value)} placeholder="optional unless API requires it" />
            </label>
            <p className="mt-2 text-xs text-zinc-400">{operatorSecret ? 'Operator secret loaded.' : 'No operator secret stored.'}</p>
            <p className="mt-2 text-xs text-zinc-400">This is the only dashboard route that edits the operator secret.</p>
          </div>

          <div className="hud-panel hud-corners hud-panel--flat hud-accent-amber hud-compact-card">
            <p className="hud-label">Webhook secret</p>
            <p className="mt-1 text-xs leading-5 text-zinc-300">Optional helper field for webhook tests. Not persisted.</p>
            <label className="mt-2 grid gap-1 text-xs text-zinc-300">
              <span className="hud-kicker">Webhook test secret</span>
              <input className="hud-input" type="password" value={webhookSecret} onChange={(event) => setWebhookSecret(event.target.value)} placeholder="optional" />
            </label>
            <p className="mt-2 text-xs text-zinc-400">{webhookSecret ? 'Webhook secret ready.' : 'Webhook secret empty.'}</p>
          </div>
        </div>
      </section>

      <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
        <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
          <div>
            <p className="hud-label">Shared dashboard config</p>
            <p className="hud-copy mt-1 text-xs text-zinc-300">Push or pull non-secret dashboard preferences through the API. Operator and webhook secrets stay out of shared config.</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <button className="hud-action-button hud-action-button--compact" type="button" onClick={() => void loadSharedConfig()}>
              Load shared
            </button>
            <button className="hud-action-button hud-action-button--compact" type="button" onClick={() => void saveSharedConfig()}>
              Save shared
            </button>
          </div>
        </div>
        {sharedConfigStatus ? <p className="mt-3 text-xs text-zinc-300">{sharedConfigStatus}</p> : null}
      </section>

      <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
        <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
          <div>
            <p className="hud-label">OBS import</p>
            <p className="hud-copy mt-1 text-xs text-zinc-300">Import live OBS scene/source names into built-in quick actions. If auto-match misses, assign them manually.</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <button className="hud-action-button hud-action-button--compact" type="button" onClick={() => void loadOBSAssignments()}>
              Load from OBS
            </button>
            <button className="hud-action-button hud-action-button--compact" type="button" disabled={!obsAssignments.mainScene && !obsAssignments.brbScene && !obsAssignments.alertsSource} onClick={applyOBSAssignments}>
              Apply assignments
            </button>
            <button className="hud-action-button hud-action-button--compact" type="button" onClick={validateOBSAssignments}>
              Validate quick actions
            </button>
          </div>
        </div>
        <div className="mt-3 grid gap-3 md:grid-cols-3">
          <AssignmentSelect label="Main scene" value={obsAssignments.mainScene} options={obsSceneNames} onChange={(mainScene) => setOBSAssignments((current) => ({ ...current, mainScene }))} />
          <AssignmentSelect label="BRB / standby scene" value={obsAssignments.brbScene} options={obsSceneNames} onChange={(brbScene) => setOBSAssignments((current) => ({ ...current, brbScene }))} />
          <AssignmentSelect label="Alerts browser source" value={obsAssignments.alertsSource} options={obsBrowserInputNames} onChange={(alertsSource) => setOBSAssignments((current) => ({ ...current, alertsSource }))} />
        </div>
        <div className="mt-3 flex flex-wrap gap-2">
          <span className={`hud-chip hud-chip--square ${obsValidationIssues.length === 0 && (obsSceneNames.length > 0 || obsInputNames.length > 0) ? 'hud-accent-emerald' : obsValidationIssues.length > 0 ? 'hud-accent-rose' : ''}`}>
            {obsSceneNames.length || obsInputNames.length ? `${obsValidationIssues.length} OBS issues` : 'OBS not loaded'}
          </span>
          <span className="hud-chip hud-chip--square">{obsSceneNames.length} scenes</span>
          <span className="hud-chip hud-chip--square">{obsInputNames.length} inputs</span>
        </div>
        {obsValidationIssues.length > 0 ? (
          <ul className="mt-3 grid gap-1 rounded-2xl border border-rose-400/30 bg-rose-400/10 px-3 py-2 text-xs text-rose-100">
            {obsValidationIssues.slice(0, 8).map((issue) => (
              <li key={`${issue.actionId}:${issue.field}:${issue.value}`}>{issue.label}: {issue.message}</li>
            ))}
          </ul>
        ) : null}
        {obsImportStatus ? <p className="mt-3 text-xs text-zinc-300">{obsImportStatus}</p> : null}
      </section>

      <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
        <p className="hud-label">Overlay noise controls</p>
        <p className="hud-copy mt-1 text-xs text-zinc-300">Tune how many live items stay visible and how aggressively repeated alerts are suppressed.</p>
        <div className="mt-3 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <NumberControl label="Alert max visible" value={config.display.alertMaxVisible} onChange={(value) => updateDisplay('alertMaxVisible', value)} />
          <NumberControl label="Alert dedupe seconds" value={config.display.alertDedupeSeconds} onChange={(value) => updateDisplay('alertDedupeSeconds', value)} />
          <NumberControl label="Chat max visible" value={config.display.chatMaxVisible} onChange={(value) => updateDisplay('chatMaxVisible', value)} />
          <NumberControl label="Ticker max items" value={config.display.tickerMaxItems} onChange={(value) => updateDisplay('tickerMaxItems', value)} />
        </div>
      </section>

      <AutomationProfileManager config={config} onChange={updateConfig} />

      <TwitchInteractionsManager apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} />

      <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
        <p className="hud-label">MPRIS decision</p>
        <p className="hud-copy mt-1 text-xs text-zinc-300">Use host MPRIS now-playing, keep it disabled, or decide later.</p>
        <div className="mt-3 grid gap-2 md:grid-cols-3">
          {(['enabled', 'disabled', 'undecided'] as const).map((value) => (
            <button key={value} className={`hud-panel hud-corners hud-panel--flat hud-compact-card text-left transition hover:border-cyan-200/50 ${config.mprisDecision === value ? 'hud-accent-cyan border-cyan-200/50' : ''}`} type="button" onClick={() => setMprisDecision(value)}>
              <span className="hud-kicker">MPRIS</span>
              <span className="mt-1 block text-sm font-bold capitalize text-white">{value}</span>
              {config.mprisDecision === value ? <span className="hud-badge mt-2 inline-flex">selected</span> : null}
            </button>
          ))}
        </div>
      </section>
    </DashboardShell>
  );
}

type OBSAssignments = {
  mainScene: string;
  brbScene: string;
  alertsSource: string;
};

function AssignmentSelect({ label, value, options, onChange }: { label: string; value: string; options: string[]; onChange: (value: string) => void }) {
  return (
    <label className="grid gap-2 text-xs text-zinc-300">
      <span className="hud-kicker">{label}</span>
      <select className="hud-input" value={value} onChange={(event) => onChange(event.target.value)}>
        <option value="">Assign…</option>
        {options.map((option) => <option key={option} value={option}>{option}</option>)}
      </select>
    </label>
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
    <label className="grid gap-2 text-xs text-zinc-300">
      <span className="hud-kicker">{label}</span>
      <input className="hud-input" type="number" min="0" value={value} onChange={(event) => onChange(Number(event.target.value))} />
    </label>
  );
}
