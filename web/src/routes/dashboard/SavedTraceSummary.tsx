import type { SavedOperatorTrace } from '../../lib/operatorTraces';
import { MetadataTraceSteps } from './MetadataTraceSteps';
import { KeyValues, Notice } from './ui';

export function SavedTraceSummary({ trace }: { trace: SavedOperatorTrace }) {
  return <section aria-label="Verified saved trace" className="grid min-w-0 gap-3">
    <Notice role="status">Saved metadata trace: {trace.status} · {trace.reason}. This is the stored snapshot; it was not re-evaluated or executed by this read.</Notice>
    <div className="break-all"><KeyValues items={[
      ['Stored trace digest', trace.digest], ['Saved at', trace.createdAt], ['Original run', trace.runId], ['Trace ID', trace.id], ['Mode', trace.mode], ['Selected version', trace.versionId], ['Original graph', trace.originalGraphDigest], ['Trace graph', trace.graphDigest], ['Capture digest', trace.captureDigest], ['Original clock', trace.clock],
    ]} /></div>
    <p className="text-sm">Format 1 / capture 1: empty facts and seed zero as a format placeholder. This metadata contains no action arguments or delivery receipts.</p>
    <MetadataTraceSteps steps={trace.steps} droppedSteps={trace.droppedSteps} />
  </section>;
}
