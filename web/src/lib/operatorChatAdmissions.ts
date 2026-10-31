import { versionStorageKey, type VersionScope } from './automationVersions';
import { decodeChatPlanIntent, type ChatPlanIntent, type ChatPlanMetadata } from './operatorChatPlans';
import { operatorTimestamp, sameOperatorInstant } from './operatorRehearsal';

// Frozen from verified plan metadata. A submitted handle permits outcome
// lookup only; it is never an automatic delivery retry.
export type ChatAdmissionIntent = { format: 1; phase: 'prepared' | 'submitted'; plan: ChatPlanIntent; sourceId: string; runId: string; digest: string; effectCount: number; preparedAt: string; deadline: string };
export type ChatAdmissionReceipt = { sourceId: string; actionIds: string[]; deadline: string; admittedAt: string; created: boolean; state: 'admitted_queued' };
export type ChatActionFailure = '' | 'retryable' | 'unauthorized' | 'unsupported' | 'quota_limited' | 'plan_limited' | 'permanent';
export type ChatActionOutcome = { actionId: string; sequence: number; state: 'pending' | 'running' | 'delivered' | 'uncertain' | 'rejected'; code: string; failure: ChatActionFailure; retryAfterNs: number; notBefore: string; deadline: string; delayed: boolean };
export type ChatCancelReceipt = { sourceId: string; canceledActionIds: string[]; canceledAt: string; state: 'canceled_unclaimed' };
export type ChatAdmissionStatus = { sourceId: string; runId: string; digest: string; deadline: string; admittedAt: string; actions: ChatActionOutcome[] };

const fail = (): never => { throw new Error('Invalid chat delivery metadata. Keep the original intent and check delivery by its original request.'); };
function record(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) return fail(); return value as Record<string, unknown>; }
function hash(value: unknown): string { return typeof value === 'string' && /^[a-f0-9]{64}$/.test(value) ? value : fail(); }
function count(value: unknown): number { return Number.isInteger(value) && Number(value) >= 1 && Number(value) <= 16 ? Number(value) : fail(); }
// Same nanosecond comparison as the plan codec; Date.parse alone drops precision.
function instant(value: string): bigint {
  const fraction = (value.match(/\.(\d+)/)?.[1] || '').padEnd(9, '0');
  return BigInt(Date.parse(value)) * 1000000n + BigInt(fraction.slice(3));
}
// Admission must fall inside the original prepared/deadline interval.
function admittedWithin(value: unknown, intent: ChatAdmissionIntent): string {
  const at = operatorTimestamp(value);
  return instant(at) >= instant(intent.preparedAt) && instant(at) < instant(intent.deadline) ? at : fail();
}
function actionIDs(value: unknown, expected: number): string[] {
  if (!Array.isArray(value) || value.length !== expected) return fail();
  const ids = value.map(hash);
  return new Set(ids).size === ids.length ? ids : fail();
}

export function chatAdmissionStorageKey(scope: VersionScope): string { return `${versionStorageKey(scope)}.operator-chat-admission`; }

export function freezeChatAdmissionIntent(plan: ChatPlanIntent | null, metadata: ChatPlanMetadata | null): ChatAdmissionIntent | null {
  if (!plan || plan.phase !== 'submitted' || !metadata || metadata.state !== 'prepared_metadata' || metadata.automationId !== plan.source.automationId || metadata.versionId !== plan.source.versionId || metadata.graphDigest !== plan.source.graphDigest) return null;
  return { format: 1, phase: 'prepared', plan: { ...plan, source: { ...plan.source } }, sourceId: metadata.sourceId, runId: metadata.runId, digest: metadata.digest, effectCount: metadata.effectCount, preparedAt: metadata.preparedAt, deadline: metadata.deadline };
}

export function decodeChatAdmissionIntent(raw: string | null): ChatAdmissionIntent | null {
  if (raw === null) return null;
  try {
    if (raw.length > 4096) return fail();
    const r = record(JSON.parse(raw));
    const plan = decodeChatPlanIntent(JSON.stringify(r.plan));
    if (r.format !== 1 || !['prepared', 'submitted'].includes(String(r.phase)) || !plan || plan.phase !== 'submitted') return fail();
    const preparedAt = operatorTimestamp(r.preparedAt); const deadline = operatorTimestamp(r.deadline);
    if (instant(preparedAt) >= instant(deadline)) return fail();
    return { format: 1, phase: r.phase as ChatAdmissionIntent['phase'], plan, sourceId: hash(r.sourceId), runId: hash(r.runId), digest: hash(r.digest), effectCount: count(r.effectCount), preparedAt, deadline };
  } catch { throw new Error('Saved chat delivery intent is unreadable. Explicitly acknowledge local state before queueing another delivery.'); }
}

// Plan metadata freshly looked up before delivery must still match the frozen
// reviewed identity; otherwise nothing is sent.
export function sameReviewedPlan(intent: ChatAdmissionIntent, metadata: ChatPlanMetadata): boolean {
  return metadata.sourceId === intent.sourceId && metadata.runId === intent.runId && metadata.digest === intent.digest && metadata.effectCount === intent.effectCount && sameOperatorInstant(metadata.deadline, intent.deadline) && sameOperatorInstant(metadata.preparedAt, intent.preparedAt);
}

// Acknowledges queue admission only, never delivery.
export function decodeChatAdmissionReceipt(value: unknown, intent: ChatAdmissionIntent): ChatAdmissionReceipt {
  const r = record(value);
  if (r.sourceId !== intent.sourceId || r.state !== 'admitted_queued' || typeof r.created !== 'boolean' || !sameOperatorInstant(operatorTimestamp(r.deadline), intent.deadline)) return fail();
  return { sourceId: intent.sourceId, actionIds: actionIDs(r.actionIds, intent.effectCount), deadline: intent.deadline, admittedAt: admittedWithin(r.admittedAt, intent), created: r.created, state: 'admitted_queued' };
}

// Each reply keeps its own fixed window from the original occurrence: it never
// ends before the plan deadline and is offset by at most 24 hours.
const maxDelayNs = 24n * 3600n * 1000000000n;
function window(r: Record<string, unknown>, intent: ChatAdmissionIntent): { notBefore: string; deadline: string; delayed: boolean } {
  const notBefore = operatorTimestamp(r.notBefore); const deadline = operatorTimestamp(r.deadline);
  const offset = instant(deadline) - instant(intent.deadline);
  if (instant(notBefore) >= instant(deadline) || offset < 0n || offset > maxDelayNs) return fail();
  return { notBefore, deadline, delayed: offset > 0n };
}

// Mirrors the server's validOperatorChatOutcome combinations.
function outcome(raw: unknown, intent: ChatAdmissionIntent): ChatActionOutcome {
  const r = record(raw);
  const retry = r.retryAfter;
  if (!Number.isSafeInteger(retry) || Number(retry) < 0 || !Number.isSafeInteger(r.sequence) || Number(r.sequence) < 1) return fail();
  const state = String(r.state); const code = String(r.code); const failure = String(r.failure); const zero = retry === 0;
  const valid =
    (state === 'pending' && code === 'admitted' && failure === '' && zero) ||
    (state === 'running' && code === 'started' && failure === '' && zero) ||
    (state === 'delivered' && code === 'delivered' && failure === '' && zero) ||
    (state === 'uncertain' && (code === 'uncertain' || code === 'deadline_elapsed') && failure === '' && zero) ||
    (state === 'rejected' && (code === 'expired' || code === 'connection_unavailable') && failure === 'permanent' && zero) ||
    (state === 'rejected' && code === 'canceled' && failure === '' && zero) ||
    (state === 'rejected' && code === 'rejected' && (failure === 'plan_limited' ? zero : ['retryable', 'unauthorized', 'unsupported', 'quota_limited', 'permanent'].includes(failure)));
  if (!valid || typeof r.state !== 'string' || typeof r.code !== 'string' || typeof r.failure !== 'string') return fail();
  return { actionId: hash(r.actionId), sequence: Number(r.sequence), state: state as ChatActionOutcome['state'], code, failure: failure as ChatActionFailure, retryAfterNs: Number(retry), ...window(r, intent) };
}

export function decodeChatAdmissionStatus(value: unknown, intent: ChatAdmissionIntent, receipt: ChatAdmissionReceipt | null): ChatAdmissionStatus {
  const r = record(value);
  if (r.sourceId !== intent.sourceId || r.runId !== intent.runId || r.digest !== intent.digest || !sameOperatorInstant(operatorTimestamp(r.deadline), intent.deadline) || !Array.isArray(r.actions)) return fail();
  const actions = r.actions.map(a => outcome(a, intent));
  const ids = actionIDs(actions.map(a => a.actionId), intent.effectCount);
  // Original traversal order: sequences strictly increase.
  if (actions.some((a, i) => i > 0 && a.sequence <= actions[i - 1].sequence)) return fail();
  const admittedAt = admittedWithin(r.admittedAt, intent);
  if (receipt && (receipt.actionIds.join() !== ids.join() || !sameOperatorInstant(receipt.admittedAt, admittedAt))) return fail();
  return { sourceId: intent.sourceId, runId: intent.runId, digest: intent.digest, deadline: intent.deadline, admittedAt, actions };
}

// Lists only replies this call canceled; the rest need the outcome lookup.
export function decodeChatCancelReceipt(value: unknown, intent: ChatAdmissionIntent): ChatCancelReceipt {
  const r = record(value);
  if (r.sourceId !== intent.sourceId || r.state !== 'canceled_unclaimed' || !Array.isArray(r.canceledActionIds) || r.canceledActionIds.length > intent.effectCount) return fail();
  const ids = r.canceledActionIds.map(hash);
  if (new Set(ids).size !== ids.length) return fail();
  return { sourceId: intent.sourceId, canceledActionIds: ids, canceledAt: operatorTimestamp(r.canceledAt), state: 'canceled_unclaimed' };
}
