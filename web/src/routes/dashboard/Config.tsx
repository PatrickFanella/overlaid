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
      <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
        <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
          <div>
            <p className="hud-label">Operator config</p>
            <h2 className="mt-3 text-2xl font-black text-white">Config</h2>
            <p className="hud-copy mt-2 text-sm text-zinc-300">Store local operator-console preferences for this browser. Secrets stay in browser session state unless you clear them.</p>
          </div>
          <span className="hud-chip">API {apiBaseUrl}</span>
        </div>

        <div className="mt-6 grid gap-5 lg:grid-cols-2">
          <div className="hud-panel hud-corners hud-accent-fuchsia rounded-[1.3rem] p-5">
            <p className="hud-label">Operator secret</p>
            <p className="mt-2 text-sm leading-6 text-zinc-300">Used for protected dashboard actions. Saved only in this browser session, not in the app backend.</p>
            <label className="mt-4 grid gap-2 text-sm text-zinc-300">
              <span className="hud-kicker">Secret header</span>
              <input className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-fuchsia-300/30 focus:ring-4" type="password" value={operatorSecret} onChange={(event) => updateOperatorSecret(event.target.value)} placeholder="optional unless API requires it" />
            </label>
            <p className="mt-3 text-sm text-zinc-400">{operatorSecret ? 'Operator secret loaded for this tab/session.' : 'No operator secret stored for this session.'}</p>
          </div>

          <div className="hud-panel hud-corners hud-accent-amber rounded-[1.3rem] p-5">
            <p className="hud-label">Webhook secret</p>
            <p className="mt-2 text-sm leading-6 text-zinc-300">Optional helper field for webhook tests. This is intentionally not persisted.</p>
            <label className="mt-4 grid gap-2 text-sm text-zinc-300">
              <span className="hud-kicker">Webhook test secret</span>
              <input className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-amber-300/30 focus:ring-4" type="password" value={webhookSecret} onChange={(event) => setWebhookSecret(event.target.value)} placeholder="optional" />
            </label>
            <p className="mt-3 text-sm text-zinc-400">{webhookSecret ? 'Webhook secret is ready for this tab.' : 'Webhook secret field is empty.'}</p>
          </div>
        </div>
      </section>

      <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
        <p className="hud-label">MPRIS decision</p>
        <p className="hud-copy mt-2 text-sm text-zinc-300">Track whether this setup should use host MPRIS now-playing, keep it disabled, or decide later.</p>
        <div className="mt-5 grid gap-3 md:grid-cols-3">
          {(['enabled', 'disabled', 'undecided'] as const).map((value) => (
            <button key={value} className={`hud-panel hud-corners rounded-2xl p-4 text-left transition hover:border-cyan-200/50 ${config.mprisDecision === value ? 'hud-accent-cyan border-cyan-200/50' : ''}`} type="button" onClick={() => setMprisDecision(value)}>
              <span className="hud-kicker">MPRIS</span>
              <span className="mt-2 block text-lg font-semibold capitalize text-white">{value}</span>
              {config.mprisDecision === value ? <span className="hud-badge mt-3 inline-flex">selected</span> : null}
            </button>
          ))}
        </div>
      </section>
    </DashboardShell>
  );
}
