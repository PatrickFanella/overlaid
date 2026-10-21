import { versionID, type Graph } from './automationVersions';

export const versionDiffLimits = { changes: 200, visits: 8192, depth: 64, path: 1024, preview: 512 } as const;
export type VersionChange = { path: string; kind: 'added' | 'removed' | 'changed'; before?: string; after?: string; previewTruncated: boolean };
export type RootVersionDiff = { fromVersionId: string; toVersionId: string; fromGraphDigest: string; toGraphDigest: string; changes: VersionChange[]; limited: boolean };

// Compare private JSON structure only. Array positions are literal indexes;
// this does not infer node renames, flow semantics or preset equivalence.
export function compareRootVersions(from: Graph, to: Graph): RootVersionDiff {
  const fail = () => { throw new Error('Cannot compare mismatched automation snapshots. Reload the saved versions.'); };
  if (!versionID(from.automationId) || from.automationId !== to.automationId || !versionID(from.versionId) || !versionID(to.versionId) || !/^[a-f0-9]{64}$/.test(from.graphDigest) || !/^[a-f0-9]{64}$/.test(to.graphDigest)) return fail();
  const roots = [from, to].map(g => g.versions.filter(v => v.automationId === g.automationId && v.versionId === g.versionId));
  if (roots.some(r => r.length !== 1)) return fail();
  const [left, right] = roots.map(r => r[0]);
  if (!versionID(left.tenantId) || left.tenantId !== right.tenantId) return fail();
  const result: RootVersionDiff = { fromVersionId: from.versionId, toVersionId: to.versionId, fromGraphDigest: from.graphDigest, toGraphDigest: to.graphDigest, changes: [], limited: false };
  let visits = 0;
  function preview(value: unknown): { text: string; shortened: boolean } {
    const text = Array.isArray(value) ? `[${value.length} items]` : value !== null && typeof value === 'object' ? `{${Object.keys(value).length} fields}` : JSON.stringify(value) ?? 'undefined';
    return { text: text.slice(0, versionDiffLimits.preview), shortened: text.length > versionDiffLimits.preview };
  }
  function change(path: string, kind: VersionChange['kind'], a: unknown, b: unknown) {
    if (result.changes.length >= versionDiffLimits.changes) { result.limited = true; return; }
    const before = preview(a); const after = preview(b);
    result.changes.push({ path: path || '/', kind, ...(kind !== 'added' ? { before: before.text } : {}), ...(kind !== 'removed' ? { after: after.text } : {}), previewTruncated: before.shortened || after.shortened });
  }
  function walk(a: unknown, b: unknown, path: string, depth: number) {
    if (result.limited) return;
    if (++visits > versionDiffLimits.visits || depth > versionDiffLimits.depth || path.length > versionDiffLimits.path) { result.limited = true; return; }
    if (Object.is(a, b)) return;
    const aObject = a !== null && typeof a === 'object'; const bObject = b !== null && typeof b === 'object';
    if (!aObject || !bObject || Array.isArray(a) !== Array.isArray(b)) { change(path, 'changed', a, b); return; }
    const aa = a as Record<string, unknown>; const bb = b as Record<string, unknown>;
    const keys = Array.isArray(a) && Array.isArray(b) ? Array.from({ length: Math.min(Math.max(a.length, b.length), versionDiffLimits.visits + 1) }, (_, i) => String(i)) : [...new Set([...Object.keys(aa), ...Object.keys(bb)])].sort();
    for (const key of keys) {
      if (result.limited) break;
      const next = `${path}/${key.replaceAll('~', '~0').replaceAll('/', '~1')}`;
      if (next.length > versionDiffLimits.path || ++visits > versionDiffLimits.visits) { result.limited = true; break; }
      if (!Object.hasOwn(aa, key)) change(next, 'added', undefined, bb[key]);
      else if (!Object.hasOwn(bb, key)) change(next, 'removed', aa[key], undefined);
      else walk(aa[key], bb[key], next, depth + 1);
    }
  }
  walk(left, right, '', 0);
  return result;
}
