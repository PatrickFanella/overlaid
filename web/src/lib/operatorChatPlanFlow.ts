import type { VersionScope } from './automationVersions';
import { decodeChatPlanIntent, decodeChatPlanMetadata, type ChatPlanIntent, type ChatPlanMetadata } from './operatorChatPlans';
import { decodeOperatorRun, type OperatorRun, type RehearsalIntent } from './operatorRehearsal';

// Injected so state transitions are testable without a DOM. The dashboard
// passes sessionStorage under chatPlanStorageKey and versionRequest.
export type ChatPlanStorage = { get(): string | null; set(raw: string): void; remove(): void };
export type ChatPlanRequest = (operation: 'lookup' | 'prepare-chat-plan' | 'lookup-chat-plan', body: Record<string, unknown>, signal: AbortSignal) => Promise<unknown>;
// Throws when the caller's mount, scope or request has been replaced.
export type ChatPlanFence = () => void;
export type ChatPlanStep = { scope: VersionScope; intent: ChatPlanIntent; storage: ChatPlanStorage; request: ChatPlanRequest; signal: AbortSignal; active: ChatPlanFence };

export const chatPlanStorageFailure = 'Chat-plan intent storage is unavailable. No new preparation can start.';

export function loadChatPlanIntent(storage: ChatPlanStorage): { intent: ChatPlanIntent | null; error: string } {
  try { return { intent: decodeChatPlanIntent(storage.get()), error: '' }; }
  catch (e) { return { intent: null, error: e instanceof Error && e.message.startsWith('Saved chat-plan') ? e.message : chatPlanStorageFailure }; }
}

// Validates before writing so the stored handle is always readable on reload.
export function writeChatPlanIntent(storage: ChatPlanStorage, next: ChatPlanIntent | null): boolean {
  try {
    if (next) { const raw = JSON.stringify(next); if (decodeChatPlanIntent(raw)?.phase !== next.phase) return false; storage.set(raw); } else storage.remove();
    return true;
  } catch { return false; }
}

// Freezes the original submitted rehearsal source only after its accepted run
// is in hand. Nothing is sent and no new request ID is generated.
export function freezeChatPlanIntent(source: RehearsalIntent | null, run: OperatorRun | null): ChatPlanIntent | null {
  if (!source || source.phase !== 'submitted' || !run || run.automationId !== source.automationId || run.versionId !== source.versionId || run.graphDigest !== source.graphDigest) return null;
  return { format: 1, phase: 'prepared', source: { ...source }, syntaxVersion: 2 };
}

async function originalRun({ scope, intent, request, signal, active }: ChatPlanStep): Promise<OperatorRun> {
  const raw = await request('lookup', { requestId: intent.source.requestId }, signal); active();
  const run = await decodeOperatorRun(raw, scope, intent.source); active();
  return run;
}

// Sends at most one preparation POST. The submitted phase is stored first, so
// a lost response, reload or malformed receipt leaves only lookup available.
export async function submitChatPlan(step: ChatPlanStep): Promise<{ intent: ChatPlanIntent; metadata: ChatPlanMetadata }> {
  const { scope, intent, storage, request, signal, active } = step;
  if (intent.phase !== 'prepared') throw new Error('This chat-plan intent was already submitted. Check the original preparation instead.');
  // Read-only: binds the POST to the original accepted run before any write.
  const run = await originalRun(step);
  const frozen: ChatPlanIntent = { ...intent, phase: 'submitted' };
  if (!writeChatPlanIntent(storage, frozen)) throw new Error(`${chatPlanStorageFailure} The preparation was not sent.`);
  const raw = await request('prepare-chat-plan', { requestId: frozen.source.requestId, expectedVersionId: frozen.source.versionId, syntaxVersion: frozen.syntaxVersion }, signal); active();
  const metadata = await decodeChatPlanMetadata(raw, scope, frozen, run); active();
  return { intent: frozen, metadata };
}

// Read-only recovery by the original request. Missing or purged work stays
// unresolved; it never prepares again or creates a replacement request.
export async function recoverChatPlan(step: ChatPlanStep): Promise<ChatPlanMetadata> {
  const { scope, intent, request, signal, active } = step;
  if (intent.phase !== 'submitted') throw new Error('No preparation was sent for this intent; nothing to check.');
  const run = await originalRun(step);
  const raw = await request('lookup-chat-plan', { requestId: intent.source.requestId }, signal); active();
  const metadata = await decodeChatPlanMetadata(raw, scope, intent, run); active();
  return metadata;
}
