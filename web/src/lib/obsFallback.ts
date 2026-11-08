import { versionID, type VersionScope } from './automationVersions';
import { getOverlayApiBaseUrl } from './eventStream';

export type OBSCondition = 'missing' | 'present' | 'muted' | 'hidden' | 'disconnected' | 'unknown';
export type OBSRule = { automationId: string; versionId: string; digest: string; revision: number; collection: string; input: string; scene: string; enabled: boolean; latched: boolean; activeProposalId: string; updatedAt: string; latest: { condition: OBSCondition; observedAt: string; targetAvailable: boolean } | null };
export type OBSProposal = { id: string; versionId: string; graphDigest: string; observationId: string; condition: 'missing'; reason: 'input_missing'; scene: string; observedAt: string; deadline: string; state: 'pending' | 'running' | 'succeeded' | 'refused' | 'uncertain' | 'dismissed' | 'expired'; failure: string; requestId: string; approvedScene: string; startedAt: string | null; finishedAt: string | null; unchanged: boolean };
export type OBSApprovalIntent = { format: 1; accountId: string; tenantId: string; baseURL: string; requestId: string; scene: string; proposal: OBSProposal };
export type OBSStorage = { get(): string | null; set(raw: string): void; remove(): void };
export type OBSRequest = (operation: string, body: Record<string, unknown>, signal: AbortSignal) => Promise<unknown>;

function invalid(): never { throw new Error('OBS response is incomplete or contradictory. Keep the original approval and check its outcome.'); }
function object(value: unknown): Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : invalid(); }
function id(value: unknown): string { return versionID(value) ? value : invalid(); }
function hash(value: unknown): string { return typeof value === 'string' && /^[0-9a-f]{64}$/.test(value) ? value : invalid(); }
function bool(value: unknown): boolean { return typeof value === 'boolean' ? value : invalid(); }
function at(value: unknown): string {
  if (typeof value !== 'string' || value.length > 64 || !/^\d{4}-\d{2}-\d{2}T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d{1,9})?Z$/.test(value) || !Number.isFinite(Date.parse(value)) || new Date(`${value.slice(0, 10)}T00:00:00Z`).toISOString().slice(0, 10) !== value.slice(0, 10)) return invalid();
  return value;
}
export function obsName(value: unknown): value is string { return typeof value === 'string' && new TextEncoder().encode(value).length > 0 && new TextEncoder().encode(value).length <= 255 && !value.includes('\0'); }
function name(value: unknown): string { return obsName(value) ? value : invalid(); }
function condition(value: unknown): OBSCondition { return typeof value === 'string' && ['missing', 'present', 'muted', 'hidden', 'disconnected', 'unknown'].includes(value) ? value as OBSCondition : invalid(); }
export function decodeOBSCurrent(value: unknown): { available: boolean; rule: OBSRule | null } {
  const body = object(value); const available = bool(body.available);
  if (body.rule === null) return { available, rule: null };
  const r = object(body.rule);
  if (!Number.isSafeInteger(r.revision) || Number(r.revision) < 1 || r.activeProposalId !== '' && typeof r.activeProposalId !== 'string') return invalid();
  const latest = r.latest === null ? null : object(r.latest);
  return { available, rule: { automationId: id(r.automationId), versionId: id(r.versionId), digest: hash(r.digest), revision: Number(r.revision), collection: name(r.collection), input: name(r.input), scene: name(r.scene), enabled: bool(r.enabled), latched: bool(r.latched), activeProposalId: r.activeProposalId === '' ? '' : hash(r.activeProposalId), updatedAt: at(r.updatedAt), latest: latest ? { condition: condition(latest.condition), observedAt: at(latest.observedAt), targetAvailable: bool(latest.targetAvailable) } : null } };
}
export function decodeOBSProposal(value: unknown): OBSProposal {
  const r = object(object(value).receipt);
  if (r.condition !== 'missing' || r.reason !== 'input_missing' || typeof r.failure !== 'string' || !['pending', 'running', 'succeeded', 'refused', 'uncertain', 'dismissed', 'expired'].includes(String(r.state))) return invalid();
  const observedAt = at(r.observedAt); const deadline = at(r.deadline);
  const startedAt = r.startedAt === null ? null : at(r.startedAt); const finishedAt = r.finishedAt === null ? null : at(r.finishedAt);
  if (Date.parse(deadline) - Date.parse(observedAt) !== 10000 || startedAt && (Date.parse(startedAt) < Date.parse(observedAt) || Date.parse(startedAt) >= Date.parse(deadline)) || finishedAt && Date.parse(finishedAt) < Date.parse(startedAt || observedAt)) return invalid();
  const state = r.state as OBSProposal['state'];
  if (startedAt) {
    id(r.requestId); name(r.approvedScene);
    if (state === 'running' ? finishedAt !== null || r.failure !== '' : finishedAt === null || !(state === 'succeeded' && r.failure === '' || state === 'refused' && r.failure === 'guard_refused' || state === 'uncertain' && ['obs_unavailable', 'interrupted', 'completion_unknown'].includes(r.failure))) return invalid();
  } else {
    if (r.requestId !== '' || r.approvedScene !== '') return invalid();
    if (state === 'pending' ? finishedAt !== null || r.failure !== '' : finishedAt === null || !(state === 'dismissed' && r.failure === 'dismissed' || state === 'expired' && r.failure === 'expired' || state === 'refused' && ['rule_changed', 'condition_cleared'].includes(r.failure))) return invalid();
  }
  return { id: hash(r.id), versionId: id(r.versionId), graphDigest: hash(r.graphDigest), observationId: hash(r.observationId), condition: 'missing', reason: 'input_missing', scene: name(r.scene), observedAt, deadline, state, failure: r.failure, requestId: r.requestId as string, approvedScene: r.approvedScene as string, startedAt, finishedAt, unchanged: bool(r.unchanged) };
}
export function obsIntentKey(scope: VersionScope) { return `overlaid.obs-approval.v1.${JSON.stringify([scope.baseURL, scope.accountId, scope.tenantId])}`; }
export function decodeOBSIntent(raw: string | null, scope: VersionScope): OBSApprovalIntent | null {
  if (raw === null) return null;
  if (raw.length > 8192) return invalid();
  const r = object(JSON.parse(raw));
  if (r.format !== 1 || r.accountId !== scope.accountId || r.tenantId !== scope.tenantId || r.baseURL !== scope.baseURL) return invalid();
  const proposal = decodeOBSProposal({ receipt: r.proposal });
  if (proposal.state !== 'pending') return invalid();
  return { format: 1, accountId: id(r.accountId), tenantId: id(r.tenantId), baseURL: scope.baseURL, requestId: id(r.requestId), scene: name(r.scene), proposal };
}
export function matchOBSOutcome(value: unknown, intent: OBSApprovalIntent): OBSProposal {
  const r = decodeOBSProposal(value); const original = intent.proposal;
  for (const key of ['id', 'versionId', 'graphDigest', 'observationId', 'scene', 'observedAt', 'deadline'] as const) if (r[key] !== original[key]) return invalid();
  if (r.startedAt && (r.requestId !== intent.requestId || r.approvedScene !== intent.scene)) return invalid();
  return r;
}
export async function submitOBSApproval(scope: VersionScope, proposal: OBSProposal, scene: string, storage: OBSStorage, request: OBSRequest, signal: AbortSignal, active: () => void, newID: () => string = () => crypto.randomUUID()): Promise<OBSProposal> {
  active();
  if (storage.get() !== null) throw new Error('Check or acknowledge the original approval before another scene request.');
  if (proposal.state !== 'pending' || !obsName(scene)) return invalid();
  const intent: OBSApprovalIntent = { format: 1, accountId: scope.accountId, tenantId: scope.tenantId, baseURL: scope.baseURL, requestId: newID(), scene, proposal };
  const raw = JSON.stringify(intent); decodeOBSIntent(raw, scope);
  storage.set(raw);
  if (storage.get() !== raw) throw new Error('Approval storage is unavailable. No scene request was sent.');
  active();
  const result = await request('approve', { proposalId: proposal.id, versionId: proposal.versionId, requestId: intent.requestId, scene }, signal);
  active(); return matchOBSOutcome(result, intent);
}
export async function recoverOBSApproval(intent: OBSApprovalIntent, request: OBSRequest, signal: AbortSignal, active: () => void): Promise<OBSProposal> {
  const result = await request('lookup', { proposalId: intent.proposal.id }, signal); active(); return matchOBSOutcome(result, intent);
}
export async function obsFallbackRequest(scope: VersionScope, operation: string, body: Record<string, unknown>, signal: AbortSignal): Promise<unknown> {
  if (!versionID(scope.accountId) || !versionID(scope.tenantId) || !scope.csrf || signal.aborted) throw new Error('Account scope changed. Reload the workspace.');
  const response = await fetch(new URL(`/api/tenants/${scope.tenantId}/obs-fallback/${operation}`, getOverlayApiBaseUrl(scope.baseURL)), { method: 'POST', credentials: 'include', headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-Overlaid-CSRF': scope.csrf }, body: JSON.stringify(body), signal });
  if (!response.ok) throw new Error(`OBS request failed (${response.status}). Check the saved rule or original approval outcome.`);
  const result: unknown = await response.json();
  if (signal.aborted) throw new Error('OBS request canceled. Check the original approval outcome.');
  return result;
}
