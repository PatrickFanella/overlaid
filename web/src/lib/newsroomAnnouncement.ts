import { getOverlayApiBaseUrl } from './eventStream';
import { newsroomRequest } from './newsroom';

export type AnnouncementKind = 'current' | 'raffle';
export type AnnouncementScope = { baseUrl: string; tenantId: string; actorId: string };
type Intent = { version: 1; mode: 'direct' | 'retained'; requestKey: string; fingerprint: string };
type IntentStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;
export type AnnouncementResult = { message: string; unresolved: boolean };
const inFlight = new Map<string, Promise<AnnouncementResult>>();
const hash = /^[0-9a-f]{64}$/;
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

export function announcementStorageKey(scope: AnnouncementScope, kind: AnnouncementKind) {
  return `overlaid.newsroom.announcement.v1:${JSON.stringify([getOverlayApiBaseUrl(scope.baseUrl), scope.tenantId, scope.actorId, kind])}`;
}
export function hasAnnouncementIntent(scope: AnnouncementScope, kind: AnnouncementKind, storage: IntentStorage) {
  return storage.getItem(announcementStorageKey(scope, kind)) !== null;
}
// Clearing is an explicit user acknowledgement, never part of an error handler.
export function clearAnnouncementIntent(scope: AnnouncementScope, kind: AnnouncementKind, storage: IntentStorage) {
  storage.removeItem(announcementStorageKey(scope, kind));
}
function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null) throw new Error('The announcement response could not be confirmed. Keep this request unresolved.');
  return value as Record<string, unknown>;
}
function readIntent(raw: string): Intent {
  let value:Record<string,unknown>;
  try { value=object(JSON.parse(raw)); } catch { throw new Error('The saved announcement request is unavailable. Check Twitch before clearing it.'); }
  if (value.version !== 1 || !['direct', 'retained'].includes(String(value.mode)) || !uuid.test(String(value.requestKey)) || !hash.test(String(value.fingerprint))) {
    throw new Error('The saved announcement request is unavailable. Check Twitch before clearing it.');
  }
  return value as Intent;
}

export function announceNewsroom(scope: AnnouncementScope, kind: AnnouncementKind, secret: string, storage: IntentStorage): Promise<AnnouncementResult> {
  const key = announcementStorageKey(scope, kind);
  const active = inFlight.get(key);
  if (active) return active;
  const operation = runAnnouncement(scope, kind, secret, storage, key).finally(() => inFlight.delete(key));
  inFlight.set(key, operation);
  return operation;
}
async function runAnnouncement(scope: AnnouncementScope, kind: AnnouncementKind, secret: string, storage: IntentStorage, key: string): Promise<AnnouncementResult> {
  const saved = storage.getItem(key);
  const retry = saved !== null;
  let intent: Intent;
  if (saved !== null) {
    intent = readIntent(saved);
    if (intent.mode === 'direct') throw new Error('Direct delivery cannot be checked automatically. Check Twitch before clearing this request.');
  } else {
    const preview = object(await newsroomRequest<unknown>(`/announce/${kind}`, 'GET', scope.baseUrl, secret));
    if (preview.tenantId !== scope.tenantId || preview.actorId !== scope.actorId || !['direct', 'retained'].includes(String(preview.mode)) || !hash.test(String(preview.fingerprint))) {
      throw new Error('The announcement account or workspace changed. Refresh before trying again.');
    }
    intent = { version: 1, mode: preview.mode as Intent['mode'], requestKey: crypto.randomUUID(), fingerprint: preview.fingerprint as string };
    const serialized = JSON.stringify(intent);
    storage.setItem(key, serialized);
    if (storage.getItem(key) !== serialized) throw new Error('The announcement request could not be saved. Nothing was sent.');
  }
  const result = object(await newsroomRequest<unknown>(`/announce/${kind}`, 'POST', scope.baseUrl, secret, {
    mode: intent.mode, tenantId: scope.tenantId, actorId: scope.actorId,
    requestKey: intent.requestKey, fingerprint: intent.fingerprint, retry,
  }));
  const state = String(result.state);
  const queued = state === 'pending' || state === 'running';
  if (result.mode !== intent.mode || !['pending', 'running', 'delivered', 'rejected', 'uncertain'].includes(state) || result.queued !== queued || result.announced !== (state === 'delivered')) {
    throw new Error('The announcement response could not be confirmed. Keep this request unresolved.');
  }
  let failure = '';
  if (intent.mode === 'retained') {
    const receipt = object(result.receipt);
    if (!hash.test(String(receipt.actionId)) || !hash.test(String(receipt.sourceId)) || receipt.state !== state || !Number.isFinite(Date.parse(String(receipt.deadline)))) {
      throw new Error('The announcement receipt could not be confirmed. Keep this request unresolved.');
    }
    if (receipt.failure !== undefined && typeof receipt.failure !== 'string' || state !== 'rejected' && receipt.failure) {
      throw new Error('The announcement receipt could not be confirmed. Keep this request unresolved.');
    }
    failure = typeof receipt.failure === 'string' ? receipt.failure : '';
  } else if (state !== 'delivered') {
    throw new Error('Direct delivery could not be confirmed. Check Twitch before clearing this request.');
  }
  if (state === 'uncertain') return { message: 'Delivery could not be confirmed. Check Twitch before starting another announcement.', unresolved: true };
  storage.removeItem(key);
  const rejection = failure === 'plan_limited' ? "The workspace's hourly HTTP allowance was reached. The announcement was not delivered. Wait for the allowance to reset before starting a new announcement." : failure === 'quota_limited' ? 'Twitch rejected the announcement because of a rate limit. It was not delivered.' : failure === 'unauthorized' ? 'The selected Twitch identity could not be authorized. The announcement was not delivered.' : 'The announcement was not delivered.';
  return { message: queued ? 'Queued for Twitch chat. Delivery has not been confirmed.' : state === 'delivered' ? 'Posted in Twitch chat.' : rejection, unresolved: false };
}
