import type { BotNukeDryRunPayload, BotNukeDryRunResponse, BotNukeExecuteResponse } from './api';
import { requestJSON } from './apiRequest';
import { getOverlayApiBaseUrl } from './eventStream';

type Scope = { baseUrl: string; tenantId: string; actorId: string };
type StorageLike = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;
type Projection = { mode: 'direct' | 'retained'; tenantId: string; actorId: string; connectionId: string };
export type NukePreview = {
  version: 1; projection: Projection; draft: BotNukeDryRunPayload;
  display: BotNukeDryRunResponse; plan: string; fingerprint: string; expiresAt: string;
  requestKey?: string;
};
export type NukeOutcome = {
  queued: boolean; radiationOnly: boolean; radiationRuleId?: string;
  states: string[]; direct?: BotNukeExecuteResponse;
};
const invalid = 'The nuke response could not be confirmed. Keep this request saved and check Twitch.';
const hash = /^[0-9a-f]{64}$/;
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(invalid);
  return value as Record<string, unknown>;
}
function isHash(value: unknown): value is string { return typeof value === 'string' && hash.test(value); }
function date(value: unknown): value is string { return typeof value === 'string' && Number.isFinite(Date.parse(value)); }
export function nukeIntentKey(scope: Scope) { return `overlaid.nuke.intent.v1:${JSON.stringify([getOverlayApiBaseUrl(scope.baseUrl), scope.tenantId, scope.actorId])}`; }
export function clearNukeIntent(scope: Scope, storage: StorageLike) { storage.removeItem(nukeIntentKey(scope)); }
function save(scope: Scope, storage: StorageLike, preview: NukePreview) {
  const serialized = JSON.stringify(preview);
  storage.setItem(nukeIntentKey(scope), serialized);
  if (storage.getItem(nukeIntentKey(scope)) !== serialized) throw new Error('The nuke request could not be saved. Nothing was sent.');
}
function projection(value: unknown, scope: Scope): Projection {
  const p = object(value);
  if (p.tenantId !== scope.tenantId || p.actorId !== scope.actorId || typeof p.mode !== 'string' || !['direct', 'retained'].includes(p.mode) || typeof p.connectionId !== 'string' || !/^twitch:[0-9]{1,128}$/.test(p.connectionId)) throw new Error('The moderation account, workspace, mode or channel changed. Keep any request saved.');
  return { mode: p.mode as Projection['mode'], tenantId: scope.tenantId, actorId: scope.actorId, connectionId: p.connectionId };
}
function matches(value: unknown): BotNukeDryRunResponse['matches'] {
  if (!Array.isArray(value) || value.length > 250) throw new Error(invalid);
  const ids = new Set<string>();
  for (const raw of value) {
    const m = object(raw);
    if (typeof m.eventId !== 'string' || !m.eventId || ids.has(m.eventId) || typeof m.user !== 'string' || typeof m.message !== 'string') throw new Error(invalid);
    ids.add(m.eventId);
  }
  return value as BotNukeDryRunResponse['matches'];
}
export function loadNukeIntent(scope: Scope, storage: StorageLike): NukePreview | null {
  const raw = storage.getItem(nukeIntentKey(scope));
  if (raw === null) return null;
  try {
    const p = object(JSON.parse(raw));
    projection(p.projection, scope);
    const d = object(p.draft), display = object(p.display);
    if (p.version !== 1 || typeof d.pattern !== 'string' || !['delete', 'timeout'].includes(String(d.action)) || display.pattern !== d.pattern || display.action !== d.action || typeof p.plan !== 'string' || typeof p.fingerprint !== 'string' || typeof p.expiresAt !== 'string' || p.requestKey !== undefined && (typeof p.requestKey !== 'string' || !uuid.test(p.requestKey))) throw new Error(invalid);
    matches(display.matches);
    if (object(p.projection).mode === 'retained' && p.plan && (!isHash(p.fingerprint) || !date(p.expiresAt) || p.plan.length > 524288)) throw new Error(invalid);
    return p as NukePreview;
  } catch { throw new Error('The saved nuke request is unavailable. Check Twitch before clearing it.'); }
}
function request(scope: Scope, secret: string, path: string, method: 'GET' | 'POST', body?: Record<string, unknown>) {
  return requestJSON<unknown>(path, { method, body, explicitBaseUrl: scope.baseUrl, operatorSecret: secret, errorLabel: 'Nuke' });
}
export async function previewNuke(scope: Scope, draft: BotNukeDryRunPayload, secret: string, storage: StorageLike): Promise<NukePreview> {
  if (loadNukeIntent(scope, storage)?.requestKey) throw new Error('Check the saved nuke request before planning another.');
  const p = projection(await request(scope, secret, '/api/bot/moderation/action', 'GET'), scope);
  let preview: NukePreview;
  if (p.mode === 'direct') {
    const raw = object(await request(scope, secret, '/api/bot/moderation/nuke/dry-run', 'POST', { ...draft, confirm: false }));
    if (raw.pattern !== draft.pattern || raw.action !== draft.action) throw new Error(invalid);
    preview = { version: 1, projection: p, draft: { ...draft, confirm: false }, display: { matches: matches(raw.matches), pattern: draft.pattern, action: draft.action! }, plan: '', fingerprint: '', expiresAt: '' };
  } else {
    const { limit, ...rest } = draft;
    delete rest.confirm;
    const raw = object(await request(scope, secret, '/api/bot/moderation/nuke/preview', 'POST', { ...p, options: { ...rest, historyLimit: limit } }));
    const returned = projection(raw, scope);
    const o = object(raw.options);
    if (returned.mode !== p.mode || returned.connectionId !== p.connectionId || typeof o.pattern !== 'string' || o.pattern !== draft.pattern.trim() || o.action !== draft.action || o.regex !== draft.regex || o.caseSensitive !== draft.caseSensitive || o.lookbackMinutes !== draft.lookbackMinutes || o.historyLimit !== draft.limit || o.radiationMinutes !== draft.radiationMinutes || o.durationSeconds !== (draft.action === 'delete' ? 0 : draft.durationSeconds) || !date(raw.observedAt) || !date(raw.expiresAt) || Date.parse(raw.expiresAt) - Date.parse(raw.observedAt) !== 120000 || typeof raw.plan !== 'string' || raw.plan.length > 524288) throw new Error(invalid);
    const targets = matches(raw.targets);
    const admissible = targets.length > 0 || Number(o.radiationMinutes) > 0;
    if (targets.length > 50 || (admissible ? !raw.plan || !isHash(raw.fingerprint) : raw.plan !== '' || raw.fingerprint !== '')) throw new Error(invalid);
    preview = { version: 1, projection: p, draft: { ...draft, pattern: o.pattern, durationSeconds: draft.action === 'delete' ? draft.durationSeconds : Number(o.durationSeconds), confirm: false }, display: { matches: targets, pattern: o.pattern, action: draft.action! }, plan: raw.plan, fingerprint: String(raw.fingerprint), expiresAt: raw.expiresAt };
  }
  save(scope, storage, preview);
  return preview;
}
export function canSubmitNuke(preview: NukePreview | null) {
  return Boolean(preview && !preview.requestKey && (preview.projection.mode === 'retained' ? preview.plan && Date.parse(preview.expiresAt) > Date.now() : preview.display.matches.length));
}
const active = new Map<string, Promise<NukeOutcome>>();
export function submitNuke(scope: Scope, secret: string, storage: StorageLike): Promise<NukeOutcome> {
  const key = nukeIntentKey(scope);
  const current = active.get(key);
  if (current) return current;
  const promise = submit(scope, secret, storage).finally(() => active.delete(key));
  active.set(key, promise);
  return promise;
}
async function submit(scope: Scope, secret: string, storage: StorageLike): Promise<NukeOutcome> {
  const preview = loadNukeIntent(scope, storage);
  if (!preview) throw new Error('Plan and review the nuke first.');
  const retry = Boolean(preview.requestKey);
  if (preview.projection.mode === 'direct' && retry) throw new Error('Direct nuke execution cannot be checked automatically. Check Twitch before clearing this request.');
  if (!retry && !canSubmitNuke(preview)) throw new Error('This preview has expired or has no admissible work. Plan again.');
  // Capture the exact preview before the first effect request. A saved request
  // always uses lookup-only retry, even if the first POST never reached the server.
  if (!retry) { preview.requestKey = crypto.randomUUID(); save(scope, storage, preview); }
  if (preview.projection.mode === 'direct') {
    const raw = object(await request(scope, secret, '/api/bot/moderation/nuke/execute', 'POST', { ...preview.draft, confirm: true }));
    if (raw.action !== preview.draft.action || !Number.isInteger(raw.attempted) || !Number.isInteger(raw.succeeded) || !Number.isInteger(raw.failed) || Number(raw.attempted) < 0 || Number(raw.succeeded) < 0 || Number(raw.failed) < 0 || Number(raw.succeeded) + Number(raw.failed) !== raw.attempted || Number(raw.attempted) > matches(raw.matches).length) throw new Error(invalid);
    return { queued: false, radiationOnly: false, states: [], direct: raw as BotNukeExecuteResponse };
  }
  const raw = object(await request(scope, secret, '/api/bot/moderation/nuke/request', 'POST', { ...preview.projection, requestKey: preview.requestKey, plan: preview.plan, retry }));
  const receipt = object(raw.receipt);
  const ids = receipt.actionIds, entries = receipt.receipts;
  if (raw.mode !== 'retained' || !isHash(receipt.sourceId) || !date(receipt.createdAt) || !date(receipt.deadline) || Date.parse(receipt.deadline) <= Date.parse(receipt.createdAt) || typeof receipt.created !== 'boolean' || !Array.isArray(ids) || !Array.isArray(entries) || ids.length !== preview.display.matches.length || entries.length !== ids.length || ids.some(id => !isHash(id)) || new Set(ids).size !== ids.length || raw.radiationOnly !== (ids.length === 0)) throw new Error(invalid);
  const expectedRadiation = Number(preview.draft.radiationMinutes) > 0;
  if (expectedRadiation ? !isHash(receipt.radiationRuleId) : receipt.radiationRuleId !== undefined && receipt.radiationRuleId !== '') throw new Error(invalid);
  const states = entries.map((entry, i) => {
    const r = object(entry);
    if (r.actionId !== ids[i] || r.manualSourceId !== receipt.sourceId || r.eventId !== '' || r.connectionId !== preview.projection.connectionId || r.capability !== (preview.draft.action === 'delete' ? 'message.delete' : 'user.timeout') || typeof r.state !== 'string' || !['pending', 'running', 'delivered', 'rejected', 'uncertain'].includes(r.state) || typeof r.code !== 'string' || r.failure !== undefined && typeof r.failure !== 'string' || r.state !== 'rejected' && Boolean(r.failure)) throw new Error(invalid);
    return r.state;
  });
  const queued = states.some(s => s === 'pending' || s === 'running');
  if (raw.queued !== queued) throw new Error(invalid);
  // Even terminal status stays saved until explicit acknowledgment. It must
  // never leave the original preview available for a second admission.
  return { queued, radiationOnly: ids.length === 0, radiationRuleId: receipt.radiationRuleId as string | undefined, states };
}
