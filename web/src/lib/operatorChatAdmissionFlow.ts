import type { VersionScope } from './automationVersions';
import { decodeChatAdmissionIntent, decodeChatAdmissionReceipt, decodeChatAdmissionStatus, decodeChatCancelReceipt, sameReviewedPlan, type ChatAdmissionIntent, type ChatAdmissionReceipt, type ChatAdmissionStatus, type ChatCancelReceipt } from './operatorChatAdmissions';
import { recoverChatPlan, type ChatPlanFence, type ChatPlanStorage } from './operatorChatPlanFlow';

export type ChatAdmissionRequest = (operation: 'lookup' | 'lookup-chat-plan' | 'admit-chat-plan' | 'lookup-chat-admission' | 'cancel-chat-admission', body: Record<string, unknown>, signal: AbortSignal) => Promise<unknown>;
export type ChatAdmissionStep = { scope: VersionScope; intent: ChatAdmissionIntent; storage: ChatPlanStorage; request: ChatAdmissionRequest; signal: AbortSignal; active: ChatPlanFence };

export const chatAdmissionStorageFailure = 'Chat delivery intent storage is unavailable. No new delivery can be queued.';

export function loadChatAdmissionIntent(storage: ChatPlanStorage): { intent: ChatAdmissionIntent | null; error: string } {
  try { return { intent: decodeChatAdmissionIntent(storage.get()), error: '' }; }
  catch (e) { return { intent: null, error: e instanceof Error && e.message.startsWith('Saved chat delivery') ? e.message : chatAdmissionStorageFailure }; }
}

export function writeChatAdmissionIntent(storage: ChatPlanStorage, next: ChatAdmissionIntent | null): boolean {
  try {
    if (next) { const raw = JSON.stringify(next); if (decodeChatAdmissionIntent(raw)?.phase !== next.phase) return false; storage.set(raw); } else storage.remove();
    return true;
  } catch { return false; }
}

// Queues delivery at most once. The plan is re-read first and must match the
// frozen reviewed digest. The submitted phase is stored before the POST, so any
// later failure leaves only outcome lookup.
export async function submitChatAdmission(step: ChatAdmissionStep): Promise<{ intent: ChatAdmissionIntent; receipt: ChatAdmissionReceipt }> {
  const { intent, storage, request, signal, active } = step;
  if (intent.phase !== 'prepared') throw new Error('This delivery was already submitted. Check its original outcome instead.');
  // The plan re-read gets read-only transport and storage: it can never prepare.
  const plan = await recoverChatPlan({
    ...step, intent: intent.plan,
    storage: { get: () => null, set: () => { throw new Error('read-only'); }, remove: () => { throw new Error('read-only'); } },
    request: (operation, body, s) => operation === 'prepare-chat-plan' ? Promise.reject(new Error('Plan preparation is not allowed while queueing delivery.')) : request(operation, body, s),
  });
  if (!sameReviewedPlan(intent, plan)) throw new Error('The prepared plan no longer matches the reviewed digest. Delivery was not queued.');
  const frozen: ChatAdmissionIntent = { ...intent, phase: 'submitted' };
  if (!writeChatAdmissionIntent(storage, frozen)) throw new Error(`${chatAdmissionStorageFailure} Delivery was not queued.`);
  const raw = await request('admit-chat-plan', { requestId: frozen.plan.source.requestId, digest: frozen.digest }, signal); active();
  return { intent: frozen, receipt: decodeChatAdmissionReceipt(raw, frozen) };
}

// Read-only. Missing or purged outcomes stay unresolved and never admit again.
export async function recoverChatAdmission(step: ChatAdmissionStep, receipt: ChatAdmissionReceipt | null): Promise<ChatAdmissionStatus> {
  const { intent, request, signal, active } = step;
  if (intent.phase !== 'submitted') throw new Error('No delivery was queued for this intent; nothing to check.');
  const raw = await request('lookup-chat-admission', { requestId: intent.plan.source.requestId, digest: intent.digest }, signal); active();
  return decodeChatAdmissionStatus(raw, intent, receipt);
}

// Cancels only replies that have not been claimed, then reads every outcome.
// Repeating it is harmless: it never queues, retries or recalls anything.
export async function cancelChatAdmission(step: ChatAdmissionStep, receipt: ChatAdmissionReceipt | null): Promise<{ canceled: ChatCancelReceipt; status: ChatAdmissionStatus }> {
  const { intent, request, signal, active } = step;
  if (intent.phase !== 'submitted') throw new Error('No delivery was queued for this intent; nothing to cancel.');
  const raw = await request('cancel-chat-admission', { requestId: intent.plan.source.requestId, digest: intent.digest }, signal); active();
  const canceled = decodeChatCancelReceipt(raw, intent);
  const status = await recoverChatAdmission(step, receipt);
  if (canceled.canceledActionIds.some(id => status.actions.find(a => a.actionId === id)?.code !== 'canceled')) throw new Error('Cancellation result contradicts the delivery outcomes. Check the original outcome again.');
  return { canceled, status };
}
