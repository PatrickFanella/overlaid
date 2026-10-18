import type { BotModerationActionPayload } from './api';
import { requestJSON } from './apiRequest';
import { getOverlayApiBaseUrl } from './eventStream';

type Scope = { baseUrl: string; tenantId: string; actorId: string };
type StorageLike = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;
type Intent = { version: 1; mode: 'direct' | 'retained'; connectionId: string; requestKey: string; payload: BotModerationActionPayload };
export type ModerationIntentResult = { action: string; result: string; reason: string };
const active = new Map<string, Promise<ModerationIntentResult>>();
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const hash = /^[0-9a-f]{64}$/;
function isHash(value: unknown) { return typeof value === 'string' && hash.test(value); }
const invalid = 'The moderation response could not be confirmed. Keep this request unresolved and check Twitch.';
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(invalid);
  return value as Record<string, unknown>;
}
export function moderationIntentKey(scope: Scope) {
  return `overlaid.moderation.intent.v1:${JSON.stringify([getOverlayApiBaseUrl(scope.baseUrl), scope.tenantId, scope.actorId])}`;
}
export function hasModerationIntent(scope: Scope, storage: StorageLike) { return storage.getItem(moderationIntentKey(scope)) !== null; }
export function clearModerationIntent(scope: Scope, storage: StorageLike) { storage.removeItem(moderationIntentKey(scope)); }

export function runModerationIntent(scope: Scope, payload: BotModerationActionPayload, secret: string, storage: StorageLike): Promise<ModerationIntentResult> {
  const key = moderationIntentKey(scope);
  const pending = active.get(key);
  if (pending) return pending;
  const promise = run(scope, payload, secret, storage, key).finally(() => active.delete(key));
  active.set(key, promise);
  return promise;
}
async function run(scope: Scope, payload: BotModerationActionPayload, secret: string, storage: StorageLike, key: string): Promise<ModerationIntentResult> {
  const request = (method: 'GET' | 'POST', body?: Record<string, unknown>) => requestJSON<unknown>('/api/bot/moderation/action', { method, body, explicitBaseUrl: scope.baseUrl, operatorSecret: secret, errorLabel: 'Moderation' });
  const raw = storage.getItem(key);
  let intent: Intent;
  if (raw !== null) {
    let saved: Record<string, unknown>;
    try { saved = object(JSON.parse(raw)); } catch { throw new Error('The saved moderation request is unavailable. Check Twitch before clearing it.'); }
    const draft = object(saved.payload);
    if (saved.version !== 1 || typeof saved.mode !== 'string' || !['direct', 'retained'].includes(saved.mode) || typeof saved.requestKey !== 'string' || !uuid.test(saved.requestKey) || typeof saved.connectionId !== 'string' || !/^twitch:[0-9]{1,128}$/.test(saved.connectionId) || !['delete', 'timeout', 'ban', 'unban'].includes(String(draft.action))) throw new Error(invalid);
    intent = saved as Intent;
    if (intent.mode === 'direct') throw new Error('Direct moderation cannot be checked automatically. Check Twitch before clearing this request.');
  } else {
    const preview = object(await request('GET'));
    if (preview.tenantId !== scope.tenantId || preview.actorId !== scope.actorId || typeof preview.mode !== 'string' || !['direct', 'retained'].includes(preview.mode) || typeof preview.connectionId !== 'string' || !/^twitch:[0-9]{1,128}$/.test(preview.connectionId)) throw new Error('The moderation account, workspace or channel changed. Refresh before trying again.');
    intent = { version: 1, mode: preview.mode as Intent['mode'], connectionId: String(preview.connectionId), requestKey: crypto.randomUUID(), payload: { ...payload } };
    const serialized = JSON.stringify(intent);
    storage.setItem(key, serialized);
    if (storage.getItem(key) !== serialized) throw new Error('The moderation request could not be saved. Nothing was sent.');
  }
  const response = object(await request('POST', { ...intent.payload, mode: intent.mode, tenantId: scope.tenantId, actorId: scope.actorId, connectionId: intent.connectionId, requestKey: intent.requestKey, retry: raw !== null }));
  if (intent.mode === 'direct') {
    if (response.action !== intent.payload.action || response.result !== 'allowed') throw new Error(invalid);
    storage.removeItem(key);
    return { action: intent.payload.action, result: 'delivered', reason: 'Twitch confirmed this action.' };
  }
  if (typeof response.state !== 'string') throw new Error(invalid);
  const state = response.state;
  const queued = state === 'pending' || state === 'running';
  const receipt = object(response.receipt);
  const entries = receipt.receipts;
  if (response.mode !== 'retained' || !['pending', 'running', 'delivered', 'rejected', 'uncertain'].includes(state) || response.queued !== queued || !isHash(receipt.sourceId) || !Number.isFinite(Date.parse(String(receipt.deadline))) || !Array.isArray(entries) || entries.length !== 1) throw new Error(invalid);
  const entry = object(entries[0]);
  const capability = {delete:'message.delete',timeout:'user.timeout',ban:'user.ban',unban:'user.unban'}[intent.payload.action];
  if (entry.capability !== capability || entry.eventId !== '' || entry.state !== state || entry.manualSourceId !== receipt.sourceId || !isHash(entry.actionId) || entry.connectionId !== intent.connectionId || !Array.isArray(receipt.actionIds) || receipt.actionIds.length !== 1 || receipt.actionIds[0] !== entry.actionId || typeof entry.code !== 'string' || (entry.failure !== undefined && typeof entry.failure !== 'string') || state !== 'rejected' && Boolean(entry.failure)) throw new Error(invalid);
  // Pending/running and uncertain outcomes retain the intent through reload.
  // Only confirmed terminal delivery/rejection permits another action.
  if (state === 'delivered' || state === 'rejected') storage.removeItem(key);
  return { action: intent.payload.action, result: state, reason: queued ? 'Queued; Twitch delivery has not been confirmed. Check status before starting another action.' : state === 'uncertain' ? 'Delivery could not be confirmed. Check Twitch before clearing this request.' : state === 'delivered' ? 'Twitch confirmed this action.' : 'The action was not delivered. Check permissions or limits before starting another action.' };
}
