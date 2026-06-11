import { useState } from 'react';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { loadDashboardConfig, loadOperatorSecret, saveDashboardConfig, saveOperatorSecret, type DashboardConfig } from '../../lib/dashboardConfig';
import { DashboardShell } from './DashboardShell';
import { BRBControl } from './components/BRBControl';
import { ControlLayoutEditor } from './components/ControlLayoutEditor';
import { OBSControls } from './components/OBSControls';
import { QuickActionRunner } from './components/QuickActionRunner';
import { QuickActionsEditor } from './components/QuickActionsEditor';
import { SampleEventControls } from './components/SampleEventControls';

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

  const widgets = [...config.controlWidgets].filter((widget) => widget.enabled).sort((a, b) => a.order - b.order);

  return (
    <DashboardShell activePath="/dashboard/control">
      <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
        <p className="hud-label">Control</p>
        <h2 className="hud-section-title">Stream control</h2>
        <p className="hud-copy mt-1 text-xs text-zinc-300">Quick actions, BRB, samples, and OBS scene tools. Toggle, resize, and reorder modules below.</p>
        <label className="mt-3 grid gap-1 text-xs text-zinc-300 lg:max-w-sm">
          <span className="hud-kicker">Operator secret</span>
          <input
            className="hud-input"
            type="password"
            value={operatorSecret}
            onChange={(event) => updateOperatorSecret(event.target.value)}
            placeholder="loaded from session storage"
          />
        </label>
      </section>

      <ControlLayoutEditor config={config} onChange={updateConfig} />
      <div className="hud-route-grid">
        {widgets.map((widget) => {
          const compact = widget.compact;
          if (widget.id === 'quick-actions') return <QuickActionRunner key={widget.id} actions={config.quickActions} apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} isRunning={runningAction !== null} runAction={runAction} compact={compact} />;
          if (widget.id === 'sample-events') return <SampleEventControls key={widget.id} apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} isRunning={runningAction !== null} runAction={runAction} compact={compact} />;
          if (widget.id === 'brb') return <BRBControl key={widget.id} apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} isRunning={runningAction !== null} runAction={runAction} compact={compact} />;
          if (widget.id === 'obs') return <OBSControls key={widget.id} apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} isRunning={runningAction !== null} runAction={runAction} compact={compact} />;
          if (widget.id === 'editor') return <QuickActionsEditor key={widget.id} config={config} onChange={updateConfig} compact={compact} />;
          return null;
        })}
      </div>
    </DashboardShell>
  );
}
