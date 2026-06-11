import { clearOverlays, sendTestAlert, setBRBStatus, setOBSInputMute, setOBSScene } from '../../../lib/api';
import type { QuickAction } from '../../../lib/dashboardConfig';

type QuickActionRunnerProps = {
  actions: QuickAction[];
  apiBaseUrl: string;
  operatorSecret: string;
  isRunning: boolean;
  runAction: (label: string, action: () => Promise<void>) => void;
};

export function QuickActionRunner({ actions, apiBaseUrl, operatorSecret, isRunning, runAction }: QuickActionRunnerProps) {
  function execute(action: QuickAction) {
    switch (action.kind) {
      case 'setScene':
        return runAction(action.label, () => setOBSScene(action.sceneName, apiBaseUrl, operatorSecret));
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
    <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
      <p className="hud-label">Quick actions</p>
      <h2 className="mt-3 text-2xl font-black text-white">Run configured actions</h2>
      <p className="hud-copy mt-2 text-sm text-zinc-300">These presets are stored in browser local storage and execute through the same protected API helpers as the rest of the console.</p>

      <div className="hud-dashboard-grid hud-dashboard-grid--three mt-5">
        {actions.map((action) => (
          <button key={action.id} className="hud-action-button" type="button" disabled={isRunning} onClick={() => execute(action)}>
            <span className="hud-kicker">{action.kind}</span>
            <span className="mt-2 block text-lg font-semibold text-white">{action.label}</span>
          </button>
        ))}
      </div>
    </section>
  );
}
