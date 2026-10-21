import { useEffect, useMemo, useRef, useState } from 'react';
import { decodeAutomations, decodeGraph, decodeVersionDesk, decodeVersions, draftDocument, emptyVersionDesk, verifyPublication, verifyValidation, versionRequest, versionStorageKey, type AutomationItem, type Graph, type VersionDeskState, type VersionList, type VersionScope } from '../../lib/automationVersions';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { compareRootVersions, type RootVersionDiff } from '../../lib/automationVersionDiff';
import { AutomationVersionComparison } from './AutomationVersionComparison';
import { OperatorRehearsalDesk } from './OperatorRehearsalDesk';
import { DashboardShell } from './DashboardShell';
import { useAccount } from './accountSession';
import { Empty, Field, KeyValues, Notice, PageHead, Panel, Tag } from './ui';

export function DashboardAutomationsPage() {
  return <DashboardShell activePath="/dashboard/automations"><AutomationAccountDesk /></DashboardShell>;
}
function AutomationAccountDesk() {
  const { account, loading } = useAccount();
  const tenant = account.tenants.find(t => t.id === account.activeTenantId);
  const baseURL = getOverlayApiBaseUrl();
  const scope: VersionScope = useMemo(() => ({ accountId: account.accountId || '', tenantId: tenant?.id || '', csrf: account.csrfToken || '', baseURL }), [account.accountId, tenant?.id, account.csrfToken, baseURL]);
  return <div className="grid min-w-0 gap-5">
    <PageHead title="Automations" lede="Prepare immutable configuration versions. Publishing here changes saved configuration; it does not run actions or change your Chat commands." />
    {loading ? <Panel title="Automation versions"><p role="status">Loading account…</p></Panel> : !account.authenticated || !scope.accountId || !scope.csrf || !tenant ? <Panel title="Automation versions"><Notice>Sign in and select a managed workspace in <a href="/dashboard/account">Account and workspace</a> to manage versions. File-backed installations do not expose this editor.</Notice></Panel> : !['owner', 'manager', 'editor'].includes(tenant.role) ? <Panel title="Automation versions"><Notice>Configure permission is required to view automation versions in this workspace.</Notice></Panel> : <AutomationVersionDesk key={JSON.stringify(scope)} scope={scope} workspaceName={tenant.name} />}
  </div>;
}

export function AutomationVersionDesk({ scope, workspaceName }: { scope: VersionScope; workspaceName: string }) {
  const storageKey = versionStorageKey(scope);
  const [initial] = useState(() => { try { return { state: decodeVersionDesk(sessionStorage.getItem(storageKey), scope.tenantId), error: '' }; } catch (e) { return { state: { ...emptyVersionDesk }, error: e instanceof Error ? e.message : 'Tab storage is unavailable.' }; } });
  const [local, setLocal] = useState<VersionDeskState>(initial.state);
  const [storageError, setStorageError] = useState(initial.error);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [items, setItems] = useState<AutomationItem[]>([]);
  const [listed, setListed] = useState(false);
  const [versions, setVersions] = useState<VersionList | null>(null);
  const [graph, setGraph] = useState<Graph | null>(null);
  const [comparison, setComparison] = useState<RootVersionDiff | null>(null);
  const [busy, setBusy] = useState(false);
  const request = useRef<AbortController | null>(null);
  const mounted = useRef(false);
  function persist(next: VersionDeskState): boolean {
    try { const raw = JSON.stringify(next); decodeVersionDesk(raw, scope.tenantId); sessionStorage.setItem(storageKey, raw); setLocal(next); setStorageError(''); return true; }
    catch { setStorageError('Tab storage is unavailable. Writes are blocked until this workspace’s draft and intent can be saved.'); return false; }
  }
  async function run(job: (signal: AbortSignal) => Promise<void>) {
    if (request.current) return;
    const controller = new AbortController(); request.current = controller;
    setBusy(true); setError(''); setMessage('');
    const timeout = window.setTimeout(() => controller.abort(), 15_000);
    try { await job(controller.signal); }
    catch (e) { if (mounted.current) setError(controller.signal.aborted ? 'Request canceled or timed out. Draft and unresolved intent are retained; check saved state before another write.' : e instanceof Error ? e.message : 'Automation operation failed.'); }
    finally { window.clearTimeout(timeout); if (request.current === controller) { request.current = null; if (mounted.current) setBusy(false); } }
  }
  const call = (op: string, body: Record<string, unknown>, signal: AbortSignal) => versionRequest(scope, op, body, signal);
  async function refresh(signal: AbortSignal) { setComparison(null); setListed(false); setItems([]); setItems(decodeAutomations(await call('list', {}, signal))); setListed(true); }
  useEffect(() => {
    mounted.current = true;
    const controller = new AbortController(); request.current = controller; setBusy(true);
    const timeout = window.setTimeout(() => controller.abort(), 15_000);
    void versionRequest(scope, 'list', {}, controller.signal).then(value => { if (!controller.signal.aborted && mounted.current && request.current === controller) { setItems(decodeAutomations(value)); setListed(true); } }).catch(() => { if (mounted.current && request.current === controller) setError('Saved automation versions are unavailable. Refresh to retry.'); }).finally(() => { window.clearTimeout(timeout); if (request.current === controller) { request.current = null; if (mounted.current) setBusy(false); } });
    return () => { mounted.current = false; controller.abort(); request.current?.abort(); };
  }, [scope]);
  async function open(automationId: string, versionId: string | undefined, signal: AbortSignal) {
    setComparison(null);
    const list = decodeVersions(await call('list-versions', { automationId }, signal), automationId);
    const selected = versionId || list.currentVersionId;
    const loaded = decodeGraph(await call('load', { automationId, versionId: selected }, signal), scope.tenantId, automationId, selected);
    setVersions(list); setGraph(loaded);
  }
  async function compare(signal: AbortSignal) {
    setComparison(null);
    if (!graph) return;
    const list = decodeVersions(await call('list-versions', { automationId: graph.automationId }, signal), graph.automationId);
    const current = decodeGraph(await call('load', { automationId: graph.automationId, versionId: list.currentVersionId }, signal), scope.tenantId, graph.automationId, list.currentVersionId);
    setComparison(compareRootVersions(graph, current));
  }
  const blocked = busy || !!local.pending || !!storageError;
  function newDraft(copy: boolean) {
    if (blocked || local.draft) return;
    const automationId = copy && graph ? graph.automationId : crypto.randomUUID();
    const versionId = crypto.randomUUID();
    const root = copy && graph ? graph.versions.find(v => v.automationId === graph.automationId && v.versionId === graph.versionId) : null;
    const doc = root ? { ...root, versionId } : { schemaVersion: 2, tenantId: scope.tenantId, automationId, versionId, trigger: { id: 'button', kind: 'dashboard' }, capabilities: ['chat.send'], restrictions: [], admission: { groups: [], cooldownMs: 0, perStreamLimit: 0 }, root: 'root', nodes: [{ id: 'root', kind: 'action', capability: 'chat.send', arguments: [{ name: 'text', value: 'Hello' }] }] };
    persist({ format: 1, pending: null, draft: { automationId, versionId, expected: copy && versions ? versions.currentVersionId : '', text: JSON.stringify(doc, null, 2) } });
  }
  async function validate(signal: AbortSignal) {
    if (!local.draft) return;
    const digest = verifyValidation(await call('validate', { document: draftDocument(local.draft, scope.tenantId) }, signal), local.draft);
    setMessage(`Structure valid. Digest ${digest}. Presets, publication and current permissions are checked separately.`);
  }
  async function publish(signal: AbortSignal) {
    setComparison(null);
    if (!local.draft || local.pending || storageError) return;
    const draft = local.draft; const document = draftDocument(draft, scope.tenantId);
    const digest = verifyValidation(await call('validate', { document }, signal), draft);
    if (!persist({ ...local, pending: { operation: 'publish', draft, digest } })) return;
    const result = await call('publish', { document, expectedCurrentVersionId: draft.expected }, signal);
    verifyPublication(result, draft.automationId, draft.versionId, digest);
    if (!persist({ format: 1, draft: null, pending: null })) return;
    setMessage('Version published. No actions were executed. Refresh the catalogue to inspect current selection.');
  }
  async function select(signal: AbortSignal) {
    setComparison(null);
    if (!graph || !versions || blocked) return;
    const selected = versions.versions.find(v => v.versionId === graph.versionId);
    if (!selected) throw new Error('Refresh versions before selecting this version.');
    if (!persist({ ...local, pending: { operation: 'select', automationId: graph.automationId, versionId: graph.versionId, expected: versions.currentVersionId } })) return;
    const result = await call('select', { automationId: graph.automationId, versionId: graph.versionId, expectedCurrentVersionId: versions.currentVersionId }, signal);
    verifyPublication(result, graph.automationId, graph.versionId, selected.digest);
    if (!persist({ ...local, pending: null })) return;
    setMessage('Current version selected. No actions were executed. Refresh versions to inspect saved state.');
  }
  async function checkPending(signal: AbortSignal) {
    const pending = local.pending;
    if (!pending) return;
    const automationId = pending.operation === 'publish' ? pending.draft.automationId : pending.automationId;
    const result = decodeVersions(await call('list-versions', { automationId }, signal), automationId);
    const resolved = pending.operation === 'publish' ? result.versions.some(v => v.versionId === pending.draft.versionId && v.digest === pending.digest) : result.currentVersionId === pending.versionId;
    if (!resolved) throw new Error('Saved state does not confirm this intent. It remains unresolved; no replacement write was sent.');
    if (!persist({ ...local, draft: pending.operation === 'publish' ? null : local.draft, pending: null })) return;
    setMessage(pending.operation === 'publish' ? 'The original immutable version is saved. Its current selection may have changed.' : 'The requested version is current in the saved snapshot.');
  }
  const loadedRoot = graph?.versions.find(v => v.automationId === graph.automationId && v.versionId === graph.versionId);
  const loadedMetadata = versions?.versions.find(v => v.versionId === graph?.versionId);
  const rehearsalCandidate = graph && loadedMetadata && loadedRoot?.schemaVersion === 2 && (loadedRoot.trigger as Record<string, unknown> | undefined)?.kind === 'dashboard'
    ? { automationId: graph.automationId, versionId: graph.versionId, graphDigest: graph.graphDigest, documentDigest: loadedMetadata.digest }
    : null;
  return <div className="grid min-w-0 gap-5">
    <Notice>Workspace: <strong>{workspaceName}</strong>. This advanced JSON desk manages saved versions only. Actions, variables and Chat commands are not executed here.</Notice>
    {storageError ? <Notice tone="bad" role="alert">{storageError} <button className="desk-btn desk-btn--small" onClick={() => { if (window.confirm('Clear this workspace’s tab draft and unresolved intent? This does not cancel or undo a saved write.')) persist({ ...emptyVersionDesk }); }}>Clear unreadable local state</button></Notice> : null}
    {error ? <Notice tone="bad" role="alert">{error}</Notice> : null}
    {message ? <Notice tone="ok" role="status">{message}</Notice> : null}
    {busy ? <p role="status">Working on this workspace’s versions…</p> : null}
    {local.pending ? <Panel title="Unresolved version intent" description="The original identity is retained in this tab. Editing and new writes are blocked until it is resolved or explicitly acknowledged.">
      <p className="break-all">{local.pending.operation === 'publish' ? `Publish ${local.pending.draft.versionId}` : `Select ${local.pending.versionId}`}</p>
      <div className="mt-3 flex flex-wrap gap-2"><button className="desk-btn desk-btn--primary" disabled={busy} onClick={() => void run(checkPending)}>Check saved state</button><button className="desk-btn" disabled={busy || !!storageError} onClick={() => { if (window.confirm('Acknowledge this unresolved intent? This clears local bookkeeping only; it does not cancel or undo the write. Keep the original IDs if further investigation is needed.')) persist({ ...local, pending: null }); }}>Acknowledge unresolved intent</button></div>
    </Panel> : null}
    <Panel title="Automation versions" description="Discover saved automations, inspect their immutable versions and choose what to edit." actions={<><button className="desk-btn desk-btn--small" disabled={busy} onClick={() => void run(refresh)}>Refresh catalogue</button><button className="desk-btn desk-btn--small" disabled={blocked || !!local.draft} onClick={() => newDraft(false)}>New automation draft</button></>}>
      {!listed ? <Empty>Refresh to load saved automations.</Empty> : !items.length ? <Empty>No automations are saved in this workspace.</Empty> : <ul className="grid gap-3">{items.map(item => <li key={item.automationId} className="desk-sub min-w-0"><button className="desk-btn max-w-full whitespace-normal break-all text-left" disabled={busy} onClick={() => void run(signal => open(item.automationId, undefined, signal))}>{item.automationId}</button><p className="mt-2 break-all text-sm">Current {item.currentVersionId} · {item.versionCount} version{item.versionCount === 1 ? '' : 's'}</p></li>)}</ul>}
    </Panel>
    {graph && versions ? <Panel title="Saved version" actions={<><button className="desk-btn desk-btn--small" disabled={busy} onClick={() => void run(compare)}>Compare with current snapshot</button><button className="desk-btn desk-btn--small" disabled={blocked || !!local.draft} onClick={() => newDraft(true)}>Draft from loaded version</button><button className="desk-btn desk-btn--small" disabled={blocked || !!local.draft || graph.versionId === versions.currentVersionId} onClick={() => void run(select)}>Select loaded version as current</button></>}>
      <Field label="Loaded version" hint="Newest-created first. Current selection below is the snapshot from this load; comparison reads its own fresh snapshot."><select className="desk-input max-w-full" value={graph.versionId} disabled={busy} onChange={e => void run(signal => open(graph.automationId, e.target.value, signal))}>{versions.versions.map(v => <option key={v.versionId} value={v.versionId}>{v.versionId}{v.versionId === versions.currentVersionId ? ' · current at load' : ''}</option>)}</select></Field>
      <div className="mt-3 break-all"><KeyValues items={[
        ['Current at load', versions.currentVersionId], ['Loaded version', graph.versionId], ['Graph digest', graph.graphDigest], ['Pinned documents', graph.versions.length],
        ['Version created', versions.versions.find(v => v.versionId === graph.versionId)?.createdAt || 'Unavailable'],
      ]} /></div>
      <details className="desk-disclosure mt-3"><summary>Exact pinned documents</summary><pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all text-xs">{JSON.stringify(graph.versions, null, 2)}</pre></details>
    </Panel> : null}
    {comparison ? <AutomationVersionComparison report={comparison} /> : null}
    {local.draft ? <Panel title="Version draft" description="Saved in this tab for this account, workspace and API. UUIDs are fixed for this draft. Validation checks structure; publish also checks presets and the expected current version.">
      <div className="mb-3 break-all"><KeyValues items={[["Draft version", local.draft.versionId], ["Expected current", local.draft.expected || 'Create only']]} /></div>
      <Field label="Automation document JSON" hint="Maximum 60,000 UTF-8 bytes. Leave the workspace, automation and version IDs unchanged."><textarea className="desk-input min-h-96 w-full font-mono text-sm" rows={20} maxLength={60000} spellCheck={false} value={local.draft.text} disabled={blocked} onChange={e => { if (local.draft) { setMessage(''); persist({ ...local, draft: { ...local.draft, text: e.target.value } }); } }} /></Field>
      <div className="mt-3 flex flex-wrap gap-2"><button className="desk-btn" disabled={blocked} onClick={() => void run(validate)}>Validate draft</button><button className="desk-btn desk-btn--primary" disabled={blocked} onClick={() => void run(publish)}>Publish draft</button><button className="desk-btn" disabled={blocked} onClick={() => { if (window.confirm('Discard this tab’s draft? Saved versions are unchanged.')) persist({ ...local, draft: null }); }}>Discard draft</button></div>
      <p className="mt-3 text-sm">A conflict keeps your original draft and expected pointer. Inspect saved versions before acknowledging an unresolved write. Start a fresh draft explicitly when you are ready to rebase changes.</p>
    </Panel> : <Tag>No local draft</Tag>}
    <OperatorRehearsalDesk scope={scope} admissionBlocked={busy || !!local.pending || !!storageError} candidate={rehearsalCandidate} />
  </div>;
}
