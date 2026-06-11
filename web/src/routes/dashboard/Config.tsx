import { useEffect, useState } from 'react';
import {
  loadDashboardConfig,
  loadOperatorSecret,
  saveDashboardConfig,
  saveOperatorSecret,
  type DashboardConfig,
} from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { DashboardShell } from './DashboardShell';

export function DashboardConfigPage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const [operatorSecret, setOperatorSecret] = useState('');
  const [webhookSecret, setWebhookSecret] = useState('');
  const [config, setConfig] = useState<DashboardConfig>(() => loadDashboardConfig());

  useEffect(() => {
    setOperatorSecret(loadOperatorSecret());
  }, []);

  function updateOperatorSecret(value: string) {
    setOperatorSecret(value);
    saveOperatorSecret(value);
  }

  function setMprisDecision(mprisDecision: DashboardConfig['mprisDecision']) {
    const next = { ...config, mprisDecision };
    setConfig(next);
    saveDashboardConfig(next);
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
