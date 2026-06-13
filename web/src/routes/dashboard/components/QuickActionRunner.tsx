import { clearOverlays, refreshOBSSource, sendTestAlert, setBRBStatus, setOBSInputMute, setOBSScene } from '../../../lib/api';
import type { QuickAction } from '../../../lib/dashboardConfig';
import { ConfirmableActionButton } from './ConfirmableActionButton';

type QuickActionRunnerProps = {
  actions: QuickAction[];
  apiBaseUrl: string;
  operatorSecret: string;
  isRunning: boolean;
  compact?: boolean;
  runAction: (label: string, action: () => Promise<void>) => void;
};

export function QuickActionRunner({ actions, apiBaseUrl, operatorSecret, isRunning, compact = false, runAction }: QuickActionRunnerProps) {
  function execute(action: QuickAction) {
    switch (action.kind) {
      case 'setScene':
        return runAction(action.label, () => setOBSScene(action.sceneName, apiBaseUrl, operatorSecret));
      case 'runPreset':
        return runAction(action.label, () => setOBSScene(action.sceneName, apiBaseUrl, operatorSecret));
      case 'refreshSource':
        return runAction(action.label, () => refreshOBSSource(action.inputName, apiBaseUrl, operatorSecret));
      case 'sendAlert':
        return runAction(action.label, () => sendTestAlert({ title: action.title, message: action.message, severity: action.severity }, apiBaseUrl, operatorSecret));
      case 'showBRB':
        return runAction(action.label, () => setBRBStatus({ active: true, title: action.title, message: action.message, eta: action.eta, items: action.items }, apiBaseUrl, operatorSecret));
      case 'hideBRB':
        return runAction(action.label, () => setBRBStatus({ active: false, title: 'BRB overlay', message: 'Waiting for intermission command', eta: '', items: [] }, apiBaseUrl, operatorSecret));
      case 'clearOverlays':
        return runAction(action.label, () => clearOverlays(apiBaseUrl, operatorSecret));
      case 'muteInput':
        return runAction(action.label, () => setOBSInputMute(action.inputName, action.muted, apiBaseUrl, operatorSecret));
    }
  }

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Quick actions</p>
      <h2 className="hud-section-title">Run configured actions</h2>
      {!compact ? <p className="hud-copy mt-1 text-xs text-zinc-300">Stored in browser local storage and executed through protected API helpers.</p> : null}

      <div className="hud-dashboard-grid hud-dashboard-grid--three mt-5">
        {actions.map((action) => (
          <ConfirmableActionButton key={action.id} label={action.label} confirm={action.confirm} disabled={isRunning} onRun={() => execute(action)} />
        ))}
      </div>
    </section>
  );
}
