import type { RehearsalStep } from '../../lib/operatorRehearsal';
import { Notice } from './ui';

export function MetadataTraceSteps({ steps, droppedSteps }: { steps: RehearsalStep[]; droppedSteps: number }) {
  return <>{droppedSteps ? <Notice tone="warn">Only the first {steps.length} trace steps are retained; {droppedSteps} additional steps were omitted.</Notice> : null}<h3 className="desk-sub__title">Flow steps</h3><ol className="grid gap-2">{steps.map(step => <li className="desk-sub min-w-0 break-all text-sm" key={step.sequence}>#{step.sequence} · {step.nodeId} · {step.kind} · {step.outcome} · {step.reason}<p className="desk-field__hint">{step.automationId} / {step.versionId} · parent {step.parent}</p></li>)}</ol></>;
}
