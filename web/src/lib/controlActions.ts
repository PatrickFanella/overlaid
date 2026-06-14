import type { QuickAction } from './dashboardConfig';
import { resolveQuickActionSequence } from './quickActionSequences';

type RunQuickActionSequenceResult = {
  warnings: string[];
  failures: string[];
  ran: number;
};

export function getConfirmableActionLabel(label: string, armed: boolean) {
  return armed ? `Confirm ${label}` : label;
}

export function nextConfirmableActionState({ confirm, armed }: { confirm?: boolean; armed: boolean }) {
  if (confirm && !armed) {
    return { armed: true, shouldRun: false };
  }

  return { armed: false, shouldRun: true };
}

export function getVisibleQuickActions(actions: QuickAction[], actionCatalog: QuickAction[] = actions) {
  return actions.filter((action) => action.kind !== 'sequence' || resolveQuickActionSequence(action, actionCatalog).steps.length > 0);
}

export function formatQuickActionRunResult(result: RunQuickActionSequenceResult) {
  if (result.ran === 0) {
    throw new Error(['Sequence has no runnable actions', ...result.warnings].join('; '));
  }

  if (result.failures.length > 0) {
    throw new Error([...result.failures, ...result.warnings].join('; '));
  }

  if (result.warnings.length > 0) {
    return `Action completed with warnings: ${result.warnings.join('; ')}`;
  }

  return undefined;
}
