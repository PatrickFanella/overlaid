import { versionStorageKey, type VersionScope } from './automationVersions';
import { decodeOperatorRun, decodeRehearsalIntent, operatorIdentity, operatorTimestamp, sameOperatorInstant, type OperatorRun, type RehearsalIntent } from './operatorRehearsal';

// A submitted handle permits lookup only. It is never an automatic POST retry.
export type ChatPlanIntent = { format: 1; phase: 'prepared' | 'submitted'; source: RehearsalIntent; syntaxVersion: 2 };
export type ChatPlanMetadata = { sourceId: string; runId: string; automationId: string; versionId: string; graphDigest: string; connectionId: string; channelId: string; digest: string; effectCount: number; preparedAt: string; deadline: string; unchanged: boolean; state: 'prepared_metadata' };
const fail = (): never => { throw new Error('Invalid chat-plan metadata. Keep the original intent and check preparation by its original request.'); };
function record(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) return fail(); return value as Record<string, unknown>; }
function hash(value: unknown): string { return typeof value === 'string' && /^[a-f0-9]{64}$/.test(value) ? value : fail(); }
export function chatPlanStorageKey(scope: VersionScope): string { return `${versionStorageKey(scope)}.operator-chat-plan`; }
export function decodeChatPlanIntent(raw: string | null): ChatPlanIntent | null {
  if (raw === null) return null;
  try {
    if (raw.length > 4096) return fail();
    const r = record(JSON.parse(raw));
    const source = decodeRehearsalIntent(JSON.stringify(r.source));
    if (r.format !== 1 || r.syntaxVersion !== 2 || !['prepared', 'submitted'].includes(String(r.phase)) || !source || source.phase !== 'submitted') return fail();
    return { format: 1, phase: r.phase as ChatPlanIntent['phase'], source, syntaxVersion: 2 };
  } catch { throw new Error('Saved chat-plan intent is unreadable. Explicitly acknowledge local state before preparing another plan.'); }
}
// Keep nanoseconds when checking the original exclusive deadline. Date.parse
// alone would collapse valid preparations in the same millisecond.
function instant(value: string): bigint {
  const fraction = (value.match(/\.(\d+)/)?.[1] || '').padEnd(9, '0');
  return BigInt(Date.parse(value)) * 1000000n + BigInt(fraction.slice(3));
}
export async function decodeChatPlanMetadata(value: unknown, scope: VersionScope, intent: ChatPlanIntent, accepted: OperatorRun): Promise<ChatPlanMetadata> {
  // Revalidate even in-memory handles at this external boundary.
  const frozen = decodeChatPlanIntent(JSON.stringify(intent));
  if (!frozen || frozen.phase !== 'submitted') return fail();
  const run = await decodeOperatorRun(accepted, scope, frozen.source);
  const r = record(value);
  const sourceId = await operatorIdentity(['overlaid.engine.operator-chat-source.v1', scope.tenantId, run.runId]);
  if (r.sourceId !== sourceId || r.runId !== run.runId || r.automationId !== run.automationId || r.versionId !== run.versionId || r.graphDigest !== run.graphDigest || r.state !== 'prepared_metadata' || typeof r.unchanged !== 'boolean') return fail();
  if (typeof r.channelId !== 'string' || !/^[a-zA-Z0-9_-]{1,128}$/.test(r.channelId) || r.connectionId !== `twitch:${r.channelId}` || !Number.isInteger(r.effectCount) || Number(r.effectCount) < 1 || Number(r.effectCount) > 16) return fail();
  const preparedAt = operatorTimestamp(r.preparedAt); const deadline = operatorTimestamp(r.deadline);
  if (!sameOperatorInstant(deadline, run.deadline) || instant(preparedAt) < instant(run.createdAt) || instant(preparedAt) >= instant(deadline)) return fail();
  // Whitelist fields: private text, caller identifiers and diagnostics never survive.
  return { sourceId, runId: run.runId, automationId: run.automationId, versionId: run.versionId, graphDigest: run.graphDigest, connectionId: r.connectionId, channelId: r.channelId, digest: hash(r.digest), effectCount: Number(r.effectCount), preparedAt, deadline, unchanged: r.unchanged, state: 'prepared_metadata' };
}
