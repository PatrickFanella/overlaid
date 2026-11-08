import { useEffect, useMemo, useRef, useState } from 'react';
import type { VersionScope } from '../../lib/automationVersions';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { decodeOBSCurrent, decodeOBSIntent, decodeOBSProposal, obsFallbackRequest, obsIntentKey, obsName, recoverOBSApproval, submitOBSApproval, type OBSApprovalIntent, type OBSCondition, type OBSProposal, type OBSRule, type OBSStorage } from '../../lib/obsFallback';
import { useAccount } from './accountSession';
import { Check, Empty, Field, KeyValues, Notice, Panel, Tag } from './ui';

export function OBSFallbackAccountDesk() {
  const { account, loading } = useAccount();
  const tenant = account.tenants.find(t => t.id === account.activeTenantId);
  const baseURL = getOverlayApiBaseUrl();
  const scope = useMemo<VersionScope>(() => ({ accountId: account.accountId || '', tenantId: tenant?.id || '', csrf: account.csrfToken || '', baseURL }), [account.accountId, tenant?.id, account.csrfToken, baseURL]);
  if (loading) return <Panel title="Missing-input fallback"><p role="status">Loading workspace…</p></Panel>;
  if (!account.authenticated || !scope.accountId || !scope.csrf || !tenant) return <Panel title="Missing-input fallback"><Notice>Sign in and select a managed workspace in <a href="/dashboard/account">Account and workspace</a> to configure an approval rule.</Notice></Panel>;
  if (!['owner', 'manager', 'editor'].includes(tenant.role)) return <Panel title="Missing-input fallback"><Notice>Configure permission is required to inspect and approve this workspace’s fallback rule.</Notice></Panel>;
  return <OBSFallbackDesk key={JSON.stringify(scope)} scope={scope} workspaceName={tenant.name} />;
}

// Account/workspace/CSRF changes remount the desk and abort its old requests.
export function OBSFallbackDesk({ scope, workspaceName }: { scope: VersionScope; workspaceName: string }) {
  const [storage] = useState<OBSStorage>(() => { const key = obsIntentKey(scope); return { get: () => sessionStorage.getItem(key), set: raw => sessionStorage.setItem(key, raw), remove: () => sessionStorage.removeItem(key) }; });
  const [initial] = useState(() => { try { return { intent: decodeOBSIntent(storage.get(), scope), error: '' }; } catch { return { intent: null, error: 'Saved approval is unreadable. New approvals are blocked until you explicitly acknowledge it.' }; } });
  const [intent, setIntent] = useState<OBSApprovalIntent | null>(initial.intent);
  const [storageError, setStorageError] = useState(initial.error);
  const [rule, setRule] = useState<OBSRule | null>(null);
  const [available, setAvailable] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [proposal, setProposal] = useState<OBSProposal | null>(null);
  const [collection, setCollection] = useState(''); const [input, setInput] = useState(''); const [scene, setScene] = useState('');
  const [enabled, setEnabled] = useState(true); const [override, setOverride] = useState('');
  const [sample, setSample] = useState<OBSCondition>('missing'); const [rehearsal, setRehearsal] = useState('');
  const [error, setError] = useState(''); const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false); const [disabling, setDisabling] = useState(false);
  const mounted = useRef(false); const initialized = useRef(false);
  const lastProposal = useRef('');
  const task = useRef<{ controller: AbortController; background: boolean } | null>(null);
  const revocation = useRef<AbortController | null>(null);
  const call = (operation: string, body: Record<string, unknown>, signal: AbortSignal) => obsFallbackRequest(scope, operation, body, signal);
  function syncIntent() {
    try { setIntent(decodeOBSIntent(storage.get(), scope)); setStorageError(''); }
    catch { setStorageError('Approval storage is unavailable or unreadable. Check the original outcome before another approval.'); }
  }
  async function perform(job: (signal: AbortSignal, active: () => void) => Promise<void>, background = false) {
    if (task.current) { if (background || !task.current.background) return; task.current.controller.abort(); }
    const controller = new AbortController(); const current = { controller, background }; task.current = current;
    if (!background) { setBusy(true); setError(''); setMessage(''); }
    const timer = window.setTimeout(() => controller.abort(), 15000);
    const active = () => { if (!mounted.current || controller.signal.aborted || task.current !== current) throw new Error('Workspace request changed. Check the original approval.'); };
    try { await job(controller.signal, active); }
    catch (e) { if (mounted.current && task.current === current) setError(e instanceof Error ? e.message : 'OBS operation failed. Check saved state.'); }
    finally { window.clearTimeout(timer); if (task.current === current) { task.current = null; if (mounted.current) { setBusy(false); syncIntent(); } } }
  }
  async function refresh(signal: AbortSignal, active: () => void) {
    const result = decodeOBSCurrent(await call('current', {}, signal)); active();
    setAvailable(result.available); setRule(result.rule); setLoaded(true);
    if (!initialized.current) { initialized.current = true; if (result.rule) { setCollection(result.rule.collection); setInput(result.rule.input); setScene(result.rule.scene); setEnabled(result.rule.enabled); } }
    const saved = decodeOBSIntent(storage.get(), scope);
    if (saved) { const outcome = await recoverOBSApproval(saved, call, signal, active); active(); setProposal(outcome); }
    else if (result.rule?.activeProposalId) { const p = decodeOBSProposal(await call('lookup', { proposalId: result.rule.activeProposalId }, signal)); active(); if (lastProposal.current !== p.id) { lastProposal.current = p.id; setOverride(''); } setProposal(p); }
    else setProposal(null);
  }
  useEffect(() => {
    mounted.current = true;
    void perform(refresh, true);
    const poll = window.setInterval(() => { if (!revocation.current) void perform(refresh, true); }, 3000);
    return () => { mounted.current = false; window.clearInterval(poll); task.current?.controller.abort(); revocation.current?.abort(); };
  }, [scope, storage]);
  async function save(signal: AbortSignal, active: () => void) {
    if (!loaded || !available || intent || !obsName(collection) || !obsName(input) || !obsName(scene)) return;
    const raw = await call('save', { automationId: rule?.automationId || crypto.randomUUID(), versionId: crypto.randomUUID(), expectedVersionId: rule?.versionId || '', expectedRevision: rule?.revision || 0, collection, input, scene, enabled }, signal); active();
    const saved = decodeOBSCurrent({ ...raw as object, available }); setRule(saved.rule); setMessage('Rule version saved. Scene changes require a separate approval.');
    await refresh(signal, active);
  }
  async function disable() {
    if (!rule?.enabled || revocation.current) return;
    const controller = new AbortController(); revocation.current = controller; setDisabling(true);
    const timer = window.setTimeout(() => controller.abort(), 15000);
    try {
      const result = await call('disable', { versionId: rule.versionId, revision: rule.revision }, controller.signal);
      if (!mounted.current || controller.signal.aborted || revocation.current !== controller) return;
      setRule(decodeOBSCurrent({ ...result as object, available }).rule); setEnabled(false); setMessage('Rule disabled. Pending approvals are revoked. An attempt already sent to OBS cannot be recalled.');
    } catch { if (mounted.current) setError('Disable response is unresolved. Refresh the saved rule to check its state.'); }
    finally { window.clearTimeout(timer); if (revocation.current === controller) { revocation.current = null; if (mounted.current) setDisabling(false); } }
  }
  async function approve(signal: AbortSignal, active: () => void) {
    if (!proposal || intent || storageError || !available) return;
    const selected = override || proposal.scene;
    if (!window.confirm(`Change OBS to “${selected}” for this missing-input proposal?`)) return;
    try { setProposal(await submitOBSApproval(scope, proposal, selected, storage, call, signal, active)); }
    finally { if (mounted.current) syncIntent(); }
  }
  async function check(signal: AbortSignal, active: () => void) { const saved = decodeOBSIntent(storage.get(), scope); if (saved) setProposal(await recoverOBSApproval(saved, call, signal, active)); }
  async function dismiss(signal: AbortSignal, active: () => void) { if (!proposal || intent) return; const result = decodeOBSProposal(await call('dismiss', { proposalId: proposal.id }, signal)); active(); if (result.id !== proposal.id || result.state !== 'dismissed') throw new Error('Dismissal is unresolved. Check the original proposal.'); setProposal(result); }
  async function rehearse(signal: AbortSignal, active: () => void) {
    const at = new Date().toISOString();
    const result = await call('rehearse', { state: sample, targetAvailable: true, observedAt: at, clock: at }, signal); active();
    const report = (result as { rehearsal?: { condition?: unknown; reason?: unknown; scene?: unknown; synthetic?: unknown } }).rehearsal;
    if (!report || report.synthetic !== true || report.condition !== sample || !obsName(report.scene) || typeof report.reason !== 'string' || !/^(input_(missing|present|muted|hidden|disconnected|unknown)|connection_lost)$/.test(report.reason)) throw new Error('Rehearsal response is incomplete.');
    setRehearsal(`${report.reason === 'input_missing' ? `Proposed scene: ${report.scene}` : conditionLabel[sample]}. Synthetic observation; no OBS read or scene change.`);
  }
  function clear() {
    if (busy || !window.confirm('Acknowledge and clear this tab’s approval? This changes local bookkeeping only. It does not cancel a scene attempt or resolve an unknown outcome.')) return;
    try { storage.remove(); setIntent(null); setStorageError(''); setProposal(null); } catch { setStorageError('Approval storage is unavailable.'); }
  }
  const shown = proposal || intent?.proposal;
  const fresh = shown?.state === 'pending' && Date.parse(shown.deadline) > Date.now();
  return <Panel title="Missing-input fallback" description="Watch for one input disappearing from the configured OBS collection. Review the observation before approving a fallback scene." actions={<button className="desk-btn desk-btn--danger" disabled={!rule?.enabled || disabling} onClick={() => void disable()}>{disabling ? 'Disabling…' : 'Disable rule'}</button>}>
    <div className="grid min-w-0 gap-4">
      <p className="text-sm">Workspace: <strong>{workspaceName}</strong>. Detection uses the OBS input list. Camera black frames, silence and media-file failures need separate diagnostics.</p>
      {error ? <Notice tone="bad" role="alert">{error}</Notice> : null}
      {message ? <Notice role="status">{message}</Notice> : null}
      {storageError ? <Notice tone="bad" role="alert">{storageError}<button className="desk-btn" disabled={busy} onClick={clear}>Acknowledge unreadable approval</button></Notice> : null}
      {!loaded ? <p role="status">Loading saved rule…</p> : !available ? <Notice>Live fallback observation requires managed self-host storage and a configured OBS connection. Saved receipts and disable remain available.</Notice> : null}
      <div className="grid gap-3 md:grid-cols-3">
        <Field label="OBS scene collection"><input className="desk-input" value={collection} onChange={e => setCollection(e.target.value)} disabled={busy || !!intent} /></Field>
        <Field label="Watched OBS input"><input className="desk-input" value={input} onChange={e => setInput(e.target.value)} disabled={busy || !!intent} /></Field>
        <Field label="Fallback scene"><input className="desk-input" value={scene} onChange={e => setScene(e.target.value)} disabled={busy || !!intent} /></Field>
      </div>
      <div className="flex flex-wrap items-center gap-3"><Check label="Observe after saving" checked={enabled} onChange={setEnabled} /><button className="desk-btn" disabled={busy || !!intent || !loaded || !available || !obsName(collection) || !obsName(input) || !obsName(scene)} onClick={() => void perform(save)}>Save new rule version</button><button className="desk-btn" disabled={busy} onClick={() => void perform(refresh)}>Refresh saved state</button></div>
      {rule ? <div className="grid min-w-0 gap-3 border-t border-white/10 pt-3"><Tag>{rule.enabled ? 'Observing' : 'Disabled'}{rule.latched ? ' · episode acknowledged' : ''}</Tag><div className="break-all"><KeyValues items={[
        ['Rule version', rule.versionId], ['Latest condition', rule.latest ? conditionLabel[rule.latest.condition] : 'No observation yet'], ['Observed at', rule.latest?.observedAt || '—'],
      ]} /></div></div> : loaded ? <Empty>No fallback rule is saved for this workspace.</Empty> : null}
      {shown ? <section aria-label="Fallback proposal" className="grid min-w-0 gap-3 border-t border-white/10 pt-3">
        <h3 className="desk-sub__title">Missing input · proposed scene: {shown.scene}</h3>
        <div className="break-all"><KeyValues items={[[ 'Observation', shown.observedAt ], ['Original deadline', shown.deadline], ['Rule version', shown.versionId], ['Original proposal', shown.id], ['Receipt', receiptLabel[shown.state]], ['Failure', shown.failure || '—']]} /></div>
        {!intent ? <><Field label="Override scene (optional)"><input className="desk-input" value={override} onChange={e => setOverride(e.target.value)} disabled={busy} /></Field><div className="flex flex-wrap gap-2"><button className="desk-btn desk-btn--primary" disabled={busy || !available || !rule?.enabled || !fresh || !!storageError || !obsName(override || shown.scene)} onClick={() => void perform(approve)}>Approve scene change</button><button className="desk-btn" disabled={busy || shown.state !== 'pending'} onClick={() => void perform(dismiss)}>Dismiss proposal</button></div>{shown.state === 'pending' && !fresh ? <Notice>This original proposal is stale. Refresh to inspect later observations.</Notice> : null}</> : <><Notice>Original approval: {intent.requestId}. Selected scene: {intent.scene}. The request is held in this tab and will not be submitted again.</Notice><div className="flex flex-wrap gap-2"><button className="desk-btn" disabled={busy} onClick={() => void perform(check)}>Check approval outcome</button><button className="desk-btn" disabled={busy} onClick={clear}>Acknowledge and clear approval</button></div></>}
        {shown.state === 'uncertain' || shown.state === 'running' ? <Notice tone="warn">The scene outcome is unresolved. Inspect OBS before taking another action.</Notice> : null}
      </section> : null}
      {rule ? <section aria-label="Fallback rehearsal" className="grid gap-3 border-t border-white/10 pt-3"><h3 className="desk-sub__title">Rehearse supplied evidence</h3><Field label="Synthetic input condition"><select className="desk-input" value={sample} onChange={e => setSample(e.target.value as OBSCondition)}>{Object.entries(conditionLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></Field><button className="desk-btn w-fit" disabled={busy} onClick={() => void perform(rehearse)}>Rehearse without changing OBS</button>{rehearsal ? <Notice role="status">{rehearsal}</Notice> : null}</section> : null}
    </div>
  </Panel>;
}

const conditionLabel: Record<OBSCondition, string> = { missing: 'Input absent from collection', present: 'Input present', muted: 'Input intentionally muted', hidden: 'Input outside program output', disconnected: 'OBS connection lost', unknown: 'Evidence unavailable or changed' };
const receiptLabel: Record<OBSProposal['state'], string> = { pending: 'Awaiting approval', running: 'Attempt started; outcome unconfirmed', succeeded: 'OBS acknowledged the scene change', refused: 'Refused; no approved dispatch', uncertain: 'Outcome unknown; no replay', dismissed: 'Proposal dismissed', expired: 'Original proposal expired' };
