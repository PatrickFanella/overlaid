import { useState } from 'react';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { loadDashboardConfig, loadOperatorSecret, saveDashboardConfig, saveOperatorSecret, type DashboardConfig } from '../../lib/dashboardConfig';
import { DashboardShell } from './DashboardShell';
import { BRBControl } from './components/BRBControl';
import { OBSControls } from './components/OBSControls';
import { QuickActionRunner } from './components/QuickActionRunner';
import { QuickActionsEditor } from './components/QuickActionsEditor';
import { SampleEventControls } from './components/SampleEventControls';
import { SystemStatusRail } from './components/SystemStatusRail';

export function DashboardControlPage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const [operatorSecret, setOperatorSecret] = useState(() => loadOperatorSecret());
  const [config, setConfig] = useState<DashboardConfig>(() => loadDashboardConfig());
  const [runningAction, setRunningAction] = useState<string | null>(null);

  function updateConfig(next: DashboardConfig) {
    setConfig(next);
    saveDashboardConfig(next);
  }

  function updateOperatorSecret(value: string) {
    setOperatorSecret(value);
    saveOperatorSecret(value);
  }

  function runAction(label: string, action: () => Promise<void>) {
    setRunningAction(label);
    void action()
      .catch(() => {})
      .finally(() => {
        setRunningAction((current) => (current === label ? null : current));
      });
  }

  return (
    <DashboardShell activePath="/dashboard/control">
      <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
        <p className="hud-label">Control</p>
        <h2 className="mt-3 text-2xl font-black text-white">Stream control</h2>
        <p className="hud-copy mt-2 text-sm text-zinc-300">Quick actions, live BRB controls, sample events, and OBS scene tools for the operator console.</p>
        <label className="mt-5 grid gap-2 text-sm text-zinc-300 lg:max-w-xl">
          <span className="hud-kicker">Operator secret</span>
          <input
            className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-fuchsia-300/30 focus:ring-4"
            type="password"
            value={operatorSecret}
            onChange={(event) => updateOperatorSecret(event.target.value)}
            placeholder="loaded from session storage"
          />
        </label>
      </section>

      <SystemStatusRail apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} config={config} />
      <QuickActionRunner actions={config.quickActions} apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} isRunning={runningAction !== null} runAction={runAction} />
      <SampleEventControls apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} isRunning={runningAction !== null} runAction={runAction} />
      <BRBControl apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} isRunning={runningAction !== null} runAction={runAction} />
      <OBSControls apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} isRunning={runningAction !== null} runAction={runAction} />
      <QuickActionsEditor config={config} onChange={updateConfig} />
    </DashboardShell>
  );
}
