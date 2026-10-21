import type { RootVersionDiff } from '../../lib/automationVersionDiff';
import { Empty, KeyValues, Notice, Panel } from './ui';

export function AutomationVersionComparison({ report }: { report: RootVersionDiff }) {
  return <Panel title="Root version comparison" description="Loaded version → current version at comparison time. This is a private JSON structure comparison, not an execution preview. Preset dependency contents are not compared.">
    <div className="break-all"><KeyValues items={[
      ['From loaded version', report.fromVersionId], ['To current snapshot', report.toVersionId], ['From graph digest', report.fromGraphDigest], ['To graph digest', report.toGraphDigest],
    ]} /></div>
    <p className="my-3 text-sm">Paths use JSON Pointer escapes (~0 for ~ and ~1 for /). Arrays are compared by position. Added or removed containers show a field/item count; inspect exact pinned documents for their contents.</p>
    {report.limited ? <Notice tone="warn" role="status">Comparison stopped at its row, work, depth or path limit. This is a partial report; omitted changes may remain.</Notice> : null}
    {!report.changes.length ? <Empty>{report.limited ? 'No changes found in the inspected portion.' : 'No root document differences found.'}</Empty> : <ol className="mt-3 grid min-w-0 gap-3">{report.changes.map((change, index) => <li className="desk-sub min-w-0" key={index}>
      <p className="break-all font-mono text-sm">{change.kind} · {change.path}</p>
      <div className="mt-2 grid min-w-0 gap-3 md:grid-cols-2"><div><p className="desk-field__hint">From loaded version</p><pre className="whitespace-pre-wrap break-all text-xs">{change.before ?? 'Not present'}</pre></div><div><p className="desk-field__hint">To current snapshot</p><pre className="whitespace-pre-wrap break-all text-xs">{change.after ?? 'Not present'}</pre></div></div>
      {change.previewTruncated ? <p className="mt-2 text-sm">Value preview truncated at 512 characters. Inspect the exact document for the full value.</p> : null}
    </li>)}</ol>}
  </Panel>;
}
