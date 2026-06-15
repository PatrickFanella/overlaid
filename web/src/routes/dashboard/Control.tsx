import { useRef, useState } from 'react';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { loadDashboardConfig, loadOperatorSecret, saveDashboardConfig, type DashboardConfig } from '../../lib/dashboardConfig';
import { getActiveAutomationProfile, selectQuickActionsForProfile } from '../../lib/automationProfiles';
import {
  clearSessionLog,
  createSessionLogEntry,
  loadSessionLog,
  prependSessionLogEntry,
  saveSessionLog,
  type SessionLogEntry,
} from '../../lib/sessionLog';
import { DashboardShell } from './DashboardShell';
import { BRBControl } from './components/BRBControl';
import { ControlLayoutEditor } from './components/ControlLayoutEditor';
import { OBSControls } from './components/OBSControls';
import { QuickActionRunner } from './components/QuickActionRunner';
import { QuickActionsEditor } from './components/QuickActionsEditor';
import { RehearsalRunner } from './components/RehearsalRunner';
import { SessionLogPanel } from './components/SessionLogPanel';
import { SampleEventControls } from './components/SampleEventControls';

export function DashboardControlPage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const [operatorSecret] = useState(() => loadOperatorSecret());
  const [config, setConfig] = useState<DashboardConfig>(() => loadDashboardConfig());
  const [sessionLog, setSessionLog] = useState<SessionLogEntry[]>(() => loadSessionLog());
  const sessionLogRef = useRef(sessionLog);
  const [runningAction, setRunningAction] = useState<string | null>(null);
  const activeAutomationProfile = getActiveAutomationProfile(config);
  const { actions: profileActions, warnings: profileWarnings } = selectQuickActionsForProfile(config);

  function updateConfig(next: DashboardConfig) {
    setConfig(next);
    saveDashboardConfig(next);
  }

  function recordLog(label: string, status: SessionLogEntry['status'], message: string) {
    const next = prependSessionLogEntry(sessionLogRef.current, createSessionLogEntry(label, status, message));
    sessionLogRef.current = next;
    setSessionLog(next);
    saveSessionLog(next);
  }

  function runAction(label: string, action: () => Promise<string | void>) {
    const startedAt = Date.now();
    setRunningAction(label);
    recordLog(label, 'running', 'Action started.');
    void action()
      .then((message) => {
        recordLog(label, 'success', `${message || 'Action completed.'} (${formatDuration(Date.now() - startedAt)})`);
      })
      .catch((error) => {
        const detail = error instanceof Error ? error.message : 'Action failed.';
        recordLog(label, 'error', `${detail} (${formatDuration(Date.now() - startedAt)})`);
      })
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
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <span className={`hud-chip hud-chip--square ${operatorSecret ? 'hud-accent-emerald' : 'hud-accent-rose'}`}>
            {operatorSecret ? 'operator secret loaded' : 'operator secret missing'}
          </span>
          <a className="hud-chip hud-chip--square" href="/dashboard/config">
            Edit secret
          </a>
        </div>
        <div className="mt-4 grid gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <p className="hud-label">Active profile</p>
            <span className="hud-chip hud-chip--square hud-accent-cyan">{activeAutomationProfile.title}</span>
          </div>
          <p className="text-xs text-zinc-400">{activeAutomationProfile.description}</p>
          {profileWarnings.length > 0 ? (
            <ul className="grid gap-1 rounded-2xl border border-amber-400/30 bg-amber-400/10 px-3 py-2 text-xs text-amber-100">
              {profileWarnings.map((warning) => (
                <li key={`${warning.profileId}:${warning.message}`}>{warning.message}</li>
              ))}
            </ul>
          ) : null}
        </div>
      </section>

      <ControlLayoutEditor config={config} onChange={updateConfig} />
      <RehearsalRunner apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} />
      <SessionLogPanel
        entries={sessionLog}
        onClear={() => {
          sessionLogRef.current = [];
          clearSessionLog();
          setSessionLog([]);
        }}
      />
      <div className="hud-route-grid">
        {widgets.map((widget) => {
          const compact = widget.compact;
          if (widget.id === 'quick-actions') return <QuickActionRunner key={widget.id} actions={profileActions} actionCatalog={config.quickActions} apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} isRunning={runningAction !== null} runAction={runAction} compact={compact} />;
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

function formatDuration(ms: number) {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}
