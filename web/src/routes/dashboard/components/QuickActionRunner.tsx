import { clearOverlays, refreshOBSSource, sendTestAlert, setBRBStatus, setOBSInputMute, setOBSScene } from '../../../lib/api';
import { formatQuickActionRunResult, getVisibleQuickActions } from '../../../lib/controlActions';
import type { QuickAction } from '../../../lib/dashboardConfig';
import { runQuickActionSequence, type AtomicQuickAction } from '../../../lib/quickActionSequences';
import { Notice, Panel } from '../ui';
import { ConfirmableActionButton } from './ConfirmableActionButton';

type QuickActionRunnerProps = {
  actions: QuickAction[];
  actionCatalog?: QuickAction[];
  apiBaseUrl: string;
  operatorSecret: string;
  isRunning: boolean;
  compact?: boolean;
  bare?: boolean;
  runAction: (label: string, action: () => Promise<string | void>) => void;
};

export function QuickActionRunner({ actions, actionCatalog, apiBaseUrl, operatorSecret, isRunning, compact = false, bare = false, runAction }: QuickActionRunnerProps) {
  const sequenceCatalog = actionCatalog ?? actions;
  const disabledReason = !operatorSecret ? 'Operator secret is missing. Add it in Settings before running protected quick actions.' : isRunning ? 'Another operator action is running.' : '';

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
      return formatQuickActionRunResult(result);
    });
  }

  const visibleActions = getVisibleQuickActions(actions, sequenceCatalog);

  const body = (
    <>
      {disabledReason ? <Notice tone="warn">{disabledReason}</Notice> : null}
      <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
        {visibleActions.map((action) => (
          <ConfirmableActionButton key={action.id} label={action.label} confirm={action.confirm} disabled={!!disabledReason} disabledReason={disabledReason} onRun={() => execute(action)} />
        ))}
      </div>
    </>
  );

  if (bare) {
    return <div className="grid gap-2">{body}</div>;
  }

  return (
    <Panel
      kicker="Quick actions"
      title="Run configured actions"
      description={
        compact
          ? 'Sequences run listed actions in order and log warnings.'
          : 'Saved in this browser and sent through the protected API. Sequences run listed actions in order, skip nested sequences, and write warnings to the session log.'
      }
    >
      {body}
    </Panel>
  );
}
