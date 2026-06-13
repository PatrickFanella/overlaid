import type { QuickAction } from './dashboardConfig';

export type AtomicQuickAction = Exclude<QuickAction, { kind: 'sequence' }>;
export type SequenceQuickAction = Extract<QuickAction, { kind: 'sequence' }>;

export type ResolvedQuickActionSequence = {
  steps: AtomicQuickAction[];
  warnings: string[];
};

export function resolveQuickActionSequence(sequence: QuickAction, actions: QuickAction[]): ResolvedQuickActionSequence {
  if (sequence.kind !== 'sequence') return { steps: [sequence], warnings: [] };

  const byId = new Map(actions.map((action) => [action.id, action]));
  const steps: AtomicQuickAction[] = [];
  const warnings: string[] = [];

  for (const actionId of sequence.actionIds) {
    const trimmedId = actionId.trim();
    if (!trimmedId) continue;

    const action = byId.get(trimmedId);
    if (!action) {
      warnings.push(`Missing action: ${trimmedId}`);
      continue;
    }

    if (action.kind === 'sequence') {
      warnings.push(`Nested sequence skipped: ${trimmedId}`);
      continue;
    }

    steps.push(action);
  }

  return { steps, warnings };
}

export async function runQuickActionSequence(
  sequence: SequenceQuickAction,
  actions: QuickAction[],
  executeAtomic: (action: AtomicQuickAction) => Promise<string | void>,
) {
  const resolved = resolveQuickActionSequence(sequence, actions);
  const failures: string[] = [];
  let ran = 0;

  for (const step of resolved.steps) {
    try {
      ran += 1;
      await executeAtomic(step);
    } catch (error) {
      failures.push(`${step.label}: ${error instanceof Error ? error.message : 'failed'}`);
      if (sequence.stopOnError) break;
    }
  }

  return { warnings: resolved.warnings, failures, ran };
}
