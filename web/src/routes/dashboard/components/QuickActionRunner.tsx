import { clearOverlays, refreshOBSSource, sendTestAlert, setBRBStatus, setOBSInputMute, setOBSScene } from '../../../lib/api';
import type { QuickAction } from '../../../lib/dashboardConfig';
import { resolveQuickActionSequence, runQuickActionSequence, type AtomicQuickAction } from '../../../lib/quickActionSequences';
import { ConfirmableActionButton } from './ConfirmableActionButton';

type QuickActionRunnerProps = {
  actions: QuickAction[];
  actionCatalog?: QuickAction[];
  apiBaseUrl: string;
  operatorSecret: string;
  isRunning: boolean;
  compact?: boolean;
  runAction: (label: string, action: () => Promise<string | void>) => void;
};

export function QuickActionRunner({ actions, actionCatalog, apiBaseUrl, operatorSecret, isRunning, compact = false, runAction }: QuickActionRunnerProps) {
  const sequenceCatalog = actionCatalog ?? actions;

  function executeAtomic(action: AtomicQuickAction) {
    switch (action.kind) {
      case 'setScene':
        return setOBSScene(action.sceneName, apiBaseUrl, operatorSecret);
      case 'runPreset':
        return setOBSScene(action.sceneName, apiBaseUrl, operatorSecret);
      case 'refreshSource':
        return refreshOBSSource(action.inputName, apiBaseUrl, operatorSecret);
      case 'sendAlert':
        return sendTestAlert({ title: action.title, message: action.message, severity: action.severity }, apiBaseUrl, operatorSecret);
      case 'showBRB':
        return setBRBStatus({ active: true, title: action.title, message: action.message, eta: action.eta, items: action.items }, apiBaseUrl, operatorSecret);
      case 'hideBRB':
        return setBRBStatus({ active: false, title: 'BRB overlay', message: 'Waiting for intermission command', eta: '', items: [] }, apiBaseUrl, operatorSecret);
      case 'clearOverlays':
        return clearOverlays(apiBaseUrl, operatorSecret);
      case 'muteInput':
        return setOBSInputMute(action.inputName, action.muted, apiBaseUrl, operatorSecret);
    }
  }

  function execute(action: QuickAction) {
    if (action.kind !== 'sequence') {
      return runAction(action.label, () => executeAtomic(action));
    }

    return runAction(action.label, async () => {
      const result = await runQuickActionSequence(action, sequenceCatalog, executeAtomic);
      if (result.ran === 0) {
        throw new Error(['Sequence has no runnable actions', ...result.warnings].join('; '));
      }
      if (result.failures.length > 0) {
        throw new Error([...result.failures, ...result.warnings].join('; '));
      }
      if (result.warnings.length > 0) {
        return `Action completed with warnings: ${result.warnings.join('; ')}`;
      }
    });
  }

  const visibleActions = actions.filter((action) => action.kind !== 'sequence' || resolveQuickActionSequence(action, sequenceCatalog).steps.length > 0);

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Quick actions</p>
      <h2 className="hud-section-title">Run configured actions</h2>
      <p className={`hud-copy mt-1 text-xs text-zinc-300 ${compact ? 'max-w-2xl' : ''}`}>Stored in browser local storage and executed through protected API helpers. Sequences run listed actions in order, skip nested sequences, and write warnings to the session log.</p>

      <div className="hud-dashboard-grid hud-dashboard-grid--three mt-5">
        {visibleActions.map((action) => (
          <ConfirmableActionButton key={action.id} label={action.label} confirm={action.confirm} disabled={isRunning} onRun={() => execute(action)} />
        ))}
      </div>
    </section>
  );
}
