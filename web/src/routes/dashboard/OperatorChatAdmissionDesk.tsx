import { useEffect, useRef, useState } from 'react';
import { versionRequest, type VersionScope } from '../../lib/automationVersions';
import { cancelChatAdmission, chatAdmissionStorageFailure, loadChatAdmissionIntent, recoverChatAdmission, submitChatAdmission, writeChatAdmissionIntent, type ChatAdmissionStep } from '../../lib/operatorChatAdmissionFlow';
import { chatAdmissionStorageKey, freezeChatAdmissionIntent, type ChatAdmissionIntent, type ChatAdmissionReceipt, type ChatAdmissionStatus } from '../../lib/operatorChatAdmissions';
import type { ChatPlanStorage } from '../../lib/operatorChatPlanFlow';
import type { ChatPlanIntent, ChatPlanMetadata } from '../../lib/operatorChatPlans';
import { Empty, KeyValues, Notice, Tag } from './ui';

function tabStorage(key: string): ChatPlanStorage {
  return { get: () => sessionStorage.getItem(key), set: raw => sessionStorage.setItem(key, raw), remove: () => sessionStorage.removeItem(key) };
}

// Remounted with its parent on account, workspace or CSRF changes.
export function OperatorChatAdmissionDesk({ scope, plan, metadata, blocked }: { scope: VersionScope; plan: ChatPlanIntent | null; metadata: ChatPlanMetadata | null; blocked: boolean }) {
  const [storage] = useState(() => tabStorage(chatAdmissionStorageKey(scope)));
  const [initial] = useState(() => loadChatAdmissionIntent(storage));
  const [intent, setIntent] = useState<ChatAdmissionIntent | null>(initial.intent);
  const [storageError, setStorageError] = useState(initial.error);
  const [receipt, setReceipt] = useState<ChatAdmissionReceipt | null>(null);
  const [status, setStatus] = useState<ChatAdmissionStatus | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [canceled, setCanceled] = useState<number | null>(null);
  const controller = useRef<AbortController | null>(null);
  const mounted = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; controller.current?.abort(); }; }, []);
  function persist(next: ChatAdmissionIntent | null) {
    if (!writeChatAdmissionIntent(storage, next)) { setStorageError(chatAdmissionStorageFailure); return false; }
    setIntent(next); setStorageError(''); return true;
  }
  async function perform(job: (step: ChatAdmissionStep) => Promise<void>) {
    if (controller.current || !intent) return;
    const current = new AbortController(); controller.current = current; setBusy(true); setError(''); setStatus(null); setCanceled(null);
    const timeout = window.setTimeout(() => current.abort(), 15000);
    const active = () => { if (current.signal.aborted || !mounted.current || controller.current !== current) throw new Error('Delivery request canceled. Check the original delivery outcome.'); };
    try { await job({ scope, intent, storage, request: (operation, body, signal) => versionRequest(scope, operation, body, signal), signal: current.signal, active }); }
    catch (e) { if (mounted.current) setError(current.signal.aborted ? 'Delivery request canceled or timed out. The original intent is retained; check its outcome.' : e instanceof Error ? e.message : 'Delivery operation failed.'); }
    finally { window.clearTimeout(timeout); if (controller.current === current) { controller.current = null; if (mounted.current) setBusy(false); } }
  }
  const candidate = freezeChatAdmissionIntent(plan, metadata);
  function freeze() {
    if (busy || intent || storageError || blocked || !candidate) return;
    if (persist(candidate)) { setReceipt(null); setStatus(null); setError(''); }
  }
  async function submit(step: ChatAdmissionStep) {
    if (storageError || blocked) return;
    if (!window.confirm(`Queue ${step.intent.effectCount} chat ${step.intent.effectCount === 1 ? 'reply' : 'replies'} for delivery to the connected Twitch channel? Messages may be sent and cannot be recalled. This request is sent once and is not retried automatically.`)) return;
    try { const result = await submitChatAdmission(step); setIntent(result.intent); setReceipt(result.receipt); }
    finally { if (mounted.current) { const saved = loadChatAdmissionIntent(storage); if (saved.intent) setIntent(saved.intent); } }
  }
  async function check(step: ChatAdmissionStep) { setStatus(await recoverChatAdmission(step, receipt)); }
  async function cancelUnsent(step: ChatAdmissionStep) {
    if (!window.confirm('Cancel every reply in this delivery that has not started sending? Replies already sending or sent cannot be recalled.')) return;
    const result = await cancelChatAdmission(step, receipt);
    setCanceled(result.canceled.canceledActionIds.length); setStatus(result.status);
  }
  function clear() {
    if (controller.current) return;
    if (window.confirm('Clear this tab’s delivery handle? This does not cancel queued or sent messages and does not resolve an unknown outcome. Keep the original request ID and digest for recovery.')) {
      if (persist(null)) { setReceipt(null); setStatus(null); setError(''); }
    }
  }
  return <section aria-label="Chat delivery" className="mt-5 grid min-w-0 gap-3 border-t border-white/10 pt-4">
    <h3 className="desk-sub__title">Chat delivery</h3>
    <p className="text-sm">Queue the reviewed plan’s replies through the existing retained sender. Queueing needs a ready sender and the original deadline. The queue worker checks provider authorization and pacing for each reply; a queued receipt is not a delivery receipt.</p>
    {storageError ? <Notice tone="bad" role="alert">{storageError} <button className="desk-btn desk-btn--small" disabled={busy} onClick={clear}>Acknowledge unreadable delivery state</button></Notice> : null}
    {error ? <Notice tone="bad" role="alert">{error}</Notice> : null}
    {busy ? <p role="status">Checking the original delivery…</p> : null}
    {!intent ? <div className="grid gap-3"><Empty>{candidate ? 'Hold the reviewed plan digest for delivery. Holding stores it locally and sends nothing.' : 'Prepare or check a chat plan to queue its delivery. Existing tab handles can be checked after reload.'}</Empty><button className="desk-btn w-fit" disabled={busy || blocked || !!storageError || !candidate} onClick={freeze}>Hold delivery intent</button></div> : <div className="grid min-w-0 gap-3">
      <div className="break-all"><KeyValues items={[
        ['Original request', intent.plan.source.requestId], ['Reviewed plan digest', intent.digest], ['Replies', String(intent.effectCount)], ['Original deadline', intent.deadline], ['Tab delivery state', status ? 'Outcome metadata verified' : receipt ? 'Queued receipt verified' : intent.phase === 'prepared' ? 'Held; nothing queued' : 'Submitted; check original outcome'],
      ]} /></div>
      <div className="flex flex-wrap gap-2"><button className="desk-btn desk-btn--primary" disabled={busy || blocked || !!storageError || intent.phase !== 'prepared'} onClick={() => void perform(submit)}>Queue delivery</button><button className="desk-btn" disabled={busy || intent.phase !== 'submitted'} onClick={() => void perform(check)}>Check delivery outcome</button><button className="desk-btn" disabled={busy || intent.phase !== 'submitted'} onClick={() => void perform(cancelUnsent)}>Cancel unsent replies</button><button className="desk-btn" disabled={busy} onClick={clear}>Acknowledge and clear delivery handle</button></div>
      <p className="text-sm">Before queueing, the plan is re-read and must still match this digest. The request is sent once. After a lost response, reload or error, only the read-only outcome check is available; it never queues again or creates a replacement request. Cancelling stops replies that have not started sending, including delayed ones; it is safe to repeat. Clearing changes tab bookkeeping only.</p>
    </div>}
    {receipt ? <ChatAdmissionReceiptSummary receipt={receipt} /> : null}
    {canceled !== null ? <Notice role="status">Canceled {canceled} {canceled === 1 ? 'reply' : 'replies'} that had not started. Replies already sending or sent were not recalled.</Notice> : null}
    {status ? <ChatAdmissionStatusSummary status={status} /> : null}
  </section>;
}

export function ChatAdmissionReceiptSummary({ receipt }: { receipt: ChatAdmissionReceipt }) {
  return <section aria-label="Verified queue receipt" className="grid min-w-0 gap-3">
    <Tag>{receipt.created ? 'Queued now' : 'Already queued earlier'} · not a delivery receipt</Tag>
    <div className="break-all"><KeyValues items={[['Source', receipt.sourceId], ['Queued at', receipt.admittedAt], ['Original deadline', receipt.deadline], ['Actions', receipt.actionIds.join(', ')]]} /></div>
  </section>;
}

const stateLabels: Record<string, string> = { pending: 'Queued', running: 'Sending', delivered: 'Delivered', uncertain: 'Outcome unknown', rejected: 'Not sent' };
export function ChatAdmissionStatusSummary({ status }: { status: ChatAdmissionStatus }) {
  return <section aria-label="Verified delivery outcomes" className="grid min-w-0 gap-3">
    <Notice role="status">Outcome snapshot from {status.actions.length} retained {status.actions.length === 1 ? 'action' : 'actions'}. A queued action can remain queued after the deadline; this read does not change it.</Notice>
    <div className="break-all"><KeyValues items={[['Source', status.sourceId], ['Original run', status.runId], ['Queued at', status.admittedAt], ['Original deadline', status.deadline]]} /></div>
    <ol className="grid gap-2">{status.actions.map(a => <li className="desk-sub min-w-0 break-all text-sm" key={a.actionId}>Reply {a.sequence} · {stateLabels[a.state]} · {a.code}{a.failure ? ` · ${a.failure}` : ''}{a.retryAfterNs ? ` · retry hint ${Math.ceil(a.retryAfterNs / 1e9)}s (informational)` : ''}{a.delayed ? ` · delayed: not before ${a.notBefore}, deadline ${a.deadline}` : ''}<br />{a.actionId}</li>)}</ol>
    <p className="text-sm">Uncertain outcomes are never retried automatically. Retry hints describe the retained outcome and grant no new attempt.</p>
  </section>;
}
