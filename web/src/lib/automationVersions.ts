import { getOverlayApiBaseUrl } from './eventStream';

export type VersionScope = { accountId: string; tenantId: string; csrf: string; baseURL: string };
export type AutomationItem = { automationId: string; currentVersionId: string; digest: string; versionCount: number; updatedAt: string; versionCreatedAt: string };
export type VersionItem = { versionId: string; digest: string; createdAt: string };
export type VersionList = { automationId: string; currentVersionId: string; versions: VersionItem[] };
export type Graph = { automationId: string; versionId: string; graphDigest: string; versions: Record<string, unknown>[] };
export type Draft = { automationId: string; versionId: string; expected: string; text: string };
export type PendingVersion = { operation: 'publish'; draft: Draft; digest: string } | { operation: 'select'; automationId: string; versionId: string; expected: string };
export type VersionDeskState = { format: 1; draft: Draft | null; pending: PendingVersion | null };
export const emptyVersionDesk: VersionDeskState = { format: 1, draft: null, pending: null };
const idPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
export function versionID(value: unknown): value is string { return typeof value === 'string' && idPattern.test(value) && value !== '00000000-0000-0000-0000-000000000000'; }
function invalid(): never { throw new Error('Invalid automation response. Keep any unresolved intent and check saved state.'); }
function object(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) return invalid(); return value as Record<string, unknown>; }
function id(value: unknown): string { return versionID(value) ? value : invalid(); }
function digest(value: unknown): string { return typeof value === 'string' && /^[0-9a-f]{64}$/.test(value) ? value : invalid(); }
function at(value: unknown): string {
  if (typeof value !== 'string' || value.length > 64 || !/^\d{4}-\d{2}-\d{2}T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d{1,9})?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/.test(value) || !Number.isFinite(Date.parse(value)) || new Date(`${value.slice(0, 10)}T00:00:00Z`).toISOString().slice(0, 10) !== value.slice(0, 10)) return invalid();
  return value;
}
export function decodeAutomations(value: unknown): AutomationItem[] {
  const rows = object(value).automations;
  if (!Array.isArray(rows) || rows.length > 100) return invalid();
  const seen = new Set<string>();
  return rows.map(raw => {
    const r = object(raw); const automationId = id(r.automationId);
    if (seen.has(automationId) || !Number.isInteger(r.versionCount) || Number(r.versionCount) < 1 || Number(r.versionCount) > 32) return invalid();
    seen.add(automationId);
    return { automationId, currentVersionId: id(r.currentVersionId), digest: digest(r.digest), versionCount: Number(r.versionCount), updatedAt: at(r.updatedAt), versionCreatedAt: at(r.versionCreatedAt) };
  });
}
export function decodeVersions(value: unknown, automationId: string): VersionList {
  const r = object(value);
  if (r.automationId !== automationId || !Array.isArray(r.versions) || !r.versions.length || r.versions.length > 32) return invalid();
  const seen = new Set<string>();
  const versions = r.versions.map(raw => { const v = object(raw); const versionId = id(v.versionId); if (seen.has(versionId)) return invalid(); seen.add(versionId); return { versionId, digest: digest(v.digest), createdAt: at(v.createdAt) }; });
  const currentVersionId = id(r.currentVersionId);
  if (!seen.has(currentVersionId)) return invalid();
  return { automationId, currentVersionId, versions };
}
export function decodeGraph(value: unknown, tenantId: string, automationId: string, versionId: string): Graph {
  const r = object(value);
  if (r.automationId !== automationId || r.versionId !== versionId || !Array.isArray(r.versions) || !r.versions.length || r.versions.length > 16 || JSON.stringify(r.versions).length > 262144) return invalid();
  const seen = new Set<string>();
  const versions = r.versions.map(raw => {
    const v = object(raw); const a = id(v.automationId); const b = id(v.versionId); const key = `${a}/${b}`;
    if (v.tenantId !== tenantId || seen.has(key)) return invalid();
    seen.add(key); return v;
  });
  if (!seen.has(`${automationId}/${versionId}`)) return invalid();
  return { automationId, versionId, graphDigest: digest(r.graphDigest), versions };
}
export function draftDocument(draft: Draft, tenantId: string): Record<string, unknown> {
  if (new TextEncoder().encode(draft.text).length > 60000) throw new Error('Draft exceeds the 60,000-byte editor limit.');
  let value: unknown;
  try { value = JSON.parse(draft.text); } catch { throw new Error('Draft must be valid JSON.'); }
  const doc = object(value);
  if (doc.tenantId !== tenantId || doc.automationId !== draft.automationId || doc.versionId !== draft.versionId) throw new Error('Keep the draft workspace, automation and version IDs unchanged. Start a new draft to create another identity.');
  return doc;
}
export function verifyValidation(value: unknown, draft: Draft): string {
  const r = object(value);
  if (r.validation !== 'structure_only' || r.automationId !== draft.automationId || r.versionId !== draft.versionId) return invalid();
  return digest(r.digest);
}
export function verifyPublication(value: unknown, automationId: string, versionId: string, expectedDigest: string) {
  const r = object(value);
  if (r.automationId !== automationId || r.versionId !== versionId || typeof r.unchanged !== 'boolean') return invalid();
  if (digest(r.digest) !== expectedDigest) return invalid();
  digest(r.graphDigest); at(r.createdAt);
}
export function sameDocument(a: unknown, b: unknown): boolean {
  const canonical = (v: unknown): string => Array.isArray(v) ? `[${v.map(canonical).join(',')}]` : v !== null && typeof v === 'object' ? `{${Object.entries(v).sort(([a], [b]) => a.localeCompare(b)).map(([k, x]) => `${JSON.stringify(k)}:${canonical(x)}`).join(',')}}` : JSON.stringify(v);
  return canonical(a) === canonical(b);
}
export function versionStorageKey(scope: VersionScope) { return `overlaid.automation-version-desk.v1.${JSON.stringify([scope.baseURL, scope.accountId, scope.tenantId])}`; }
export function decodeVersionDesk(raw: string | null, tenantId: string): VersionDeskState {
  if (raw === null) return { ...emptyVersionDesk };
  try {
    if (raw.length > 262144) return invalid();
    const r = object(JSON.parse(raw));
    if (r.format !== 1) return invalid();
    function draft(value: unknown): Draft {
      const d = object(value);
      if (typeof d.text !== 'string' || d.text.length > 60000 || d.expected !== '' && !versionID(d.expected)) return invalid();
      return { automationId: id(d.automationId), versionId: id(d.versionId), expected: d.expected as string, text: d.text };
    }
    const d = r.draft === null ? null : draft(r.draft);
    let pending: PendingVersion | null = null;
    if (r.pending !== null) {
      const p = object(r.pending);
      if (p.operation === 'publish') { const pd = draft(p.draft); draftDocument(pd, tenantId); if (!d || !sameDocument(pd, d)) return invalid(); pending = { operation: 'publish', draft: pd, digest: digest(p.digest) }; }
      else if (p.operation === 'select') pending = { operation: 'select', automationId: id(p.automationId), versionId: id(p.versionId), expected: id(p.expected) };
      else return invalid();
    }
    return { format: 1, draft: d, pending };
  } catch { throw new Error('Saved tab state is unreadable. Copy any available draft before explicitly clearing this workspace’s local state.'); }
}
export async function versionRequest(scope: VersionScope, operation: string, body: Record<string, unknown>, signal: AbortSignal): Promise<unknown> {
  if (!versionID(scope.accountId) || !versionID(scope.tenantId) || !scope.csrf || signal.aborted) throw new Error('The account scope changed. Reload the workspace before continuing.');
  const response = await fetch(new URL(`/api/tenants/${scope.tenantId}/automations/${operation}`, getOverlayApiBaseUrl(scope.baseURL)).toString(), { method: 'POST', credentials: 'include', headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-Overlaid-CSRF': scope.csrf }, body: JSON.stringify(body), signal });
  if (signal.aborted) throw new Error('Request canceled. Check saved state before another write.');
  if (!response.ok) throw new Error(`Automation request failed (${response.status}). Draft and unresolved intent are retained; refresh or check saved state.`);
  let value: unknown;
  try { value = await response.json(); } catch { return invalid(); }
  if (signal.aborted) throw new Error('Request canceled. Check saved state before another write.');
  return value;
}
