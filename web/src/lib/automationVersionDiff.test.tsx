import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { compareDraftRoot, compareRootVersions, versionDiffLimits } from './automationVersionDiff';
import type { Draft, Graph } from './automationVersions';
import { AutomationVersionComparison } from '../routes/dashboard/AutomationVersionComparison';

const tenant = '11111111-1111-4111-8111-111111111111';
const automation = '22222222-2222-4222-8222-222222222222';
const version = '33333333-3333-4333-8333-333333333333';
function graph(fields: Record<string, unknown> = {}): Graph { return { automationId: automation, versionId: version, graphDigest: 'a'.repeat(64), versions: [{ tenantId: tenant, automationId: automation, versionId: version, ...fields }] }; }
describe('bounded immutable root comparison', () => {
  it('compares deterministic escaped paths, arrays, null and type changes without modifying inputs', () => {
    const left = graph({ z: 1, 'a/b~c': 2, nodes: [{ id: 'one', text: 'old' }, 'removed'], type: {}, nullable: null });
    const right = graph({ type: [], nodes: [{ text: 'new', id: 'one' }], added: false, nullable: 'known' });
    const before = JSON.stringify([left, right]);
    const result = compareRootVersions(left, right);
    expect(result.limited).toBe(false);
    expect(result.changes.map(c => [c.path, c.kind])).toEqual([['/a~1b~0c', 'removed'], ['/added', 'added'], ['/nodes/0/text', 'changed'], ['/nodes/1', 'removed'], ['/nullable', 'changed'], ['/type', 'changed'], ['/z', 'removed']]);
    expect(result.changes.find(c => c.path === '/nullable')?.before).toBe('null');
    expect(JSON.stringify([left, right])).toBe(before);
    expect(compareRootVersions(left, graph({ nullable: 'known', added: false, nodes: [{ id: 'one', text: 'new' }], type: [] }))).toEqual(result);
  });
  it('does not imply dependency equivalence or semantic array alignment', () => {
    const left = graph({ nodes: [{ id: 'a' }, { id: 'b' }] });
    const right = graph({ nodes: [{ id: 'b' }, { id: 'a' }] });
    expect(compareRootVersions(left, right).changes.map(c => c.path)).toEqual(['/nodes/0/id', '/nodes/1/id']);
    const dependency = { tenantId: tenant, automationId: automation, versionId: tenant, privateText: 'DEPENDENCY' };
    expect(compareRootVersions({ ...left, versions: [...left.versions, dependency] }, left).changes).toEqual([]);
    expect(renderToStaticMarkup(<AutomationVersionComparison report={compareRootVersions(left, left)} />)).toContain('Preset dependency contents are not compared');
  });
  it('refuses foreign/missing/multiple root identities and malformed graph digests', () => {
    const right = graph();
    for (const bad of [{ ...right, automationId: tenant }, { ...right, graphDigest: 'PRIVATE' }, { ...right, versions: [] }, { ...right, versions: [...right.versions, ...right.versions] }, graph({ tenantId: automation })]) expect(() => compareRootVersions(graph(), bad)).toThrow('mismatched');
  });
  it('bounds changes, recursion, inspection, paths and value previews with visible limits', () => {
    const rows = compareRootVersions(graph({ value: [] }), graph({ value: Array.from({ length: 1000 }, (_, i) => i) }));
    expect(rows.limited).toBe(true); expect(rows.changes).toHaveLength(versionDiffLimits.changes);
    let left: Record<string, unknown> = {}; let right: Record<string, unknown> = { changed: true };
    for (let i = 0; i < 80; i++) { left = { nested: left }; right = { nested: right }; }
    const deep = compareRootVersions(graph(left), graph(right));
    expect(deep.limited).toBe(true);
    const wide = compareRootVersions(graph({ value: Array(10000).fill(0) }), graph({ value: Array(10000).fill(0) }));
    expect(wide.limited).toBe(true); expect(wide.changes).toHaveLength(0);
    expect(compareRootVersions(graph({ ['k'.repeat(1100)]: 1 }), graph()).limited).toBe(true);
    const preview = compareRootVersions(graph({ text: 'x'.repeat(5000) }), graph({ text: 'y'.repeat(5000) }));
    expect(preview.changes[0].before).toHaveLength(versionDiffLimits.preview);
    expect(preview.changes[0].previewTruncated).toBe(true);
    const html = renderToStaticMarkup(<AutomationVersionComparison report={rows} />);
    expect(html).toContain('partial report');
    expect(renderToStaticMarkup(<AutomationVersionComparison report={preview} />)).toContain('Value preview truncated');
    expect(renderToStaticMarkup(<AutomationVersionComparison report={wide} />)).not.toContain('No root document differences');
  });
  it('escapes private authored text and field paths in rendered comparisons', () => {
    const result = compareRootVersions(graph(), graph({ '<img src=x onerror=alert(1)>': '<script>private text</script>' }));
    const html = renderToStaticMarkup(<AutomationVersionComparison report={result} />);
    expect(html).toContain('&lt;script&gt;'); expect(html).toContain('&lt;img'); expect(html).not.toContain('<script>'); expect(html).not.toContain('<img');
  });
});

describe('bounded raw draft to current-root comparison', () => {
  function draft(fields: Record<string, unknown> = {}): Draft { return { automationId: automation, versionId: version, expected: version, text: JSON.stringify(graph(fields).versions[0]) }; }
  it('identifies exact raw UTF8 draft text separately from structural equality and server graph identity', async () => {
    const current = graph({ text: '雪 <script>' }); const original = draft({ text: '雪 <script>' });
    const report = await compareDraftRoot(original, tenant, current);
    const formatted = { ...original, text: JSON.stringify(JSON.parse(original.text), null, 2) };
    expect(report.changes).toEqual([]);
    expect(report.draftTextDigest).toBe('16ce9801a7ffca25f1884b573cc257f9defcf6962e494dd6d999fbad65452408');
    expect((await compareDraftRoot(formatted, tenant, current)).draftTextDigest).not.toBe(report.draftTextDigest);
    expect(original.expected).toBe(version); expect(report.toGraphDigest).toBe(current.graphDigest);
    const html = renderToStaticMarkup(<AutomationVersionComparison report={report} />);
    expect(html).toContain('Draft version comparison'); expect(html).toContain('exact raw draft text'); expect(html).not.toContain('From graph digest');
  });
  it('refuses invalid/private source headers and mismatched or missing saved roots without fabricating a graph', async () => {
    for (const bad of [{ ...draft(), expected: 'invalid' }, { ...draft(), text: '{' }, { ...draft(), text: JSON.stringify({ ...graph().versions[0], tenantId: automation }) }, { ...draft(), text: ' '.repeat(60001) }]) await expect(compareDraftRoot(bad, tenant, graph())).rejects.toThrow();
    for (const current of [{ ...graph(), automationId: tenant }, { ...graph(), versions: [] }, { ...graph(), graphDigest: 'PRIVATE' }, { ...graph(), versions: [...graph().versions, ...graph().versions] }, graph({ tenantId: automation })]) await expect(compareDraftRoot(draft(), tenant, current)).rejects.toThrow('mismatched');
  });
  it('shares root-only limits and escaped previews, while keeping stale expected pointers immutable', async () => {
    const input = draft({ text: '<img src=x onerror=alert(1)> $(user)', nodes: Array.from({ length: 500 }, (_, i) => i) });
    input.expected = automation;
    const before = JSON.stringify(input);
    const report = await compareDraftRoot(input, tenant, graph({ text: 'saved', nodes: [] }));
    expect(report.limited).toBe(true); expect(report.changes).toHaveLength(versionDiffLimits.changes);
    expect(JSON.stringify(input)).toBe(before); expect(report.expectedCurrentId).toBe(automation);
    const escaped = await compareDraftRoot(draft({ text: '<script>opaque</script>' }), tenant, graph());
    const html = renderToStaticMarkup(<AutomationVersionComparison report={escaped} />);
    expect(html).toContain('&lt;script&gt;'); expect(html).not.toContain('<script>');
    expect(renderToStaticMarkup(<AutomationVersionComparison report={report} />)).toContain('differs from the current saved snapshot');
  });
});
