import { useEffect, useState } from 'react';
import { fetchDashboardConfig, saveSharedDashboardConfig } from '../../lib/api';
import {
  loadDashboardConfig,
  loadOperatorSecret,
  saveDashboardConfig,
  saveOperatorSecret,
  type DashboardConfig,
  normalizeDashboardConfig,
} from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { DashboardShell } from './DashboardShell';
import { AutomationProfileManager } from './components/AutomationProfileManager';
import { TwitchInteractionsManager } from './components/TwitchInteractionsManager';

export function DashboardConfigPage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const [operatorSecret, setOperatorSecret] = useState('');
  const [webhookSecret, setWebhookSecret] = useState('');
  const [config, setConfig] = useState<DashboardConfig>(() => loadDashboardConfig());
  const [sharedConfigStatus, setSharedConfigStatus] = useState('');

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

function NumberControl({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) {
  return (
    <label className="grid gap-2 text-xs text-zinc-300">
      <span className="hud-kicker">{label}</span>
      <input className="hud-input" type="number" min="0" value={value} onChange={(event) => onChange(Number(event.target.value))} />
    </label>
  );
}
