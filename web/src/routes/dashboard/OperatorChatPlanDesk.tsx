import { useEffect, useRef, useState } from 'react';
import { versionRequest, type VersionScope } from '../../lib/automationVersions';
import { chatPlanStorageFailure, freezeChatPlanIntent, loadChatPlanIntent, recoverChatPlan, submitChatPlan, writeChatPlanIntent, type ChatPlanStep, type ChatPlanStorage } from '../../lib/operatorChatPlanFlow';
import { chatPlanStorageKey, type ChatPlanIntent, type ChatPlanMetadata } from '../../lib/operatorChatPlans';
import type { OperatorRun, RehearsalIntent } from '../../lib/operatorRehearsal';
import { OperatorChatAdmissionDesk } from './OperatorChatAdmissionDesk';
import { Empty, KeyValues, Notice, Tag } from './ui';

function tabStorage(key: string): ChatPlanStorage {
  return { get: () => sessionStorage.getItem(key), set: raw => sessionStorage.setItem(key, raw), remove: () => sessionStorage.removeItem(key) };
}

// The parent remounts this desk when account, workspace or CSRF scope changes,
// so a mount fence also discards late results from a previous scope.
export function OperatorChatPlanDesk({ scope, source, run, blocked }: { scope: VersionScope; source: RehearsalIntent | null; run: OperatorRun | null; blocked: boolean }) {
  const [storage] = useState(() => tabStorage(chatPlanStorageKey(scope)));
  const [initial] = useState(() => loadChatPlanIntent(storage));
  const [intent, setIntent] = useState<ChatPlanIntent | null>(initial.intent);
  const [storageError, setStorageError] = useState(initial.error);
  const [metadata, setMetadata] = useState<ChatPlanMetadata | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const controller = useRef<AbortController | null>(null);
  const mounted = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; controller.current?.abort(); }; }, []);
  function persist(next: ChatPlanIntent | null) {
    if (!writeChatPlanIntent(storage, next)) { setStorageError(chatPlanStorageFailure); return false; }
    setIntent(next); setStorageError(''); return true;
  }
  async function perform(job: (step: ChatPlanStep) => Promise<void>) {
    if (controller.current || !intent) return;
    const current = new AbortController(); controller.current = current; setBusy(true); setError(''); setMetadata(null);
    const timeout = window.setTimeout(() => current.abort(), 15000);
    const active = () => { if (current.signal.aborted || !mounted.current || controller.current !== current) throw new Error('Chat-plan request canceled. Check the original preparation.'); };
    try { await job({ scope, intent, storage, request: (operation, body, signal) => versionRequest(scope, operation, body, signal), signal: current.signal, active }); }
    catch (e) { if (mounted.current) setError(current.signal.aborted ? 'Chat-plan request canceled or timed out. The original intent is retained; check its preparation.' : e instanceof Error ? e.message : 'Chat-plan operation failed.'); }
    finally { window.clearTimeout(timeout); if (controller.current === current) { controller.current = null; if (mounted.current) setBusy(false); } }
  }
  function freeze() {
    if (busy || intent || storageError || blocked) return;
    const next = freezeChatPlanIntent(source, run);
    if (next && persist(next)) { setMetadata(null); setError(''); }
  }
  async function submit(step: ChatPlanStep) {
    if (storageError || blocked) return;
    // Show the submitted handle even if the response is lost.
    try { const result = await submitChatPlan(step); setIntent(result.intent); setMetadata(result.metadata); }
    finally { if (mounted.current) { const saved = loadChatPlanIntent(storage); if (saved.intent) setIntent(saved.intent); } }
  }
  async function check(step: ChatPlanStep) { const result = await recoverChatPlan(step); setMetadata(result); }
  function clear() {
    if (controller.current) return;
    if (window.confirm('Clear this tab’s chat-plan handle? This does not delete or cancel a prepared plan, refund capacity or resolve an unknown outcome. Keep the original request ID for recovery.')) {
      if (persist(null)) { setMetadata(null); setError(''); }
    }
  }
  const ready = !!freezeChatPlanIntent(source, run);
  return <section aria-label="Chat-plan preparation" className="mt-5 grid min-w-0 gap-3 border-t border-white/10 pt-4">
    <h3 className="desk-sub__title">Chat-plan preparation</h3>
    <p className="text-sm">Prepare the accepted occurrence’s chat replies as a retained private plan. The server renders text from the original source clock and checks the active channel connection. Preparation queues no delivery and returns metadata only.</p>
    {storageError ? <Notice tone="bad" role="alert">{storageError} <button className="desk-btn desk-btn--small" disabled={busy} onClick={clear}>Acknowledge unreadable chat-plan state</button></Notice> : null}
    {error ? <Notice tone="bad" role="alert">{error}</Notice> : null}
    {busy ? <p role="status">Checking the original chat-plan preparation…</p> : null}
    {!intent ? <div className="grid gap-3"><Empty>{ready ? 'Hold this accepted occurrence for chat-plan preparation. Holding stores the original request locally and sends nothing.' : 'Admit or check an operator occurrence to prepare its chat plan. Existing tab handles can be checked after reload.'}</Empty><button className="desk-btn w-fit" disabled={busy || blocked || !!storageError || !ready} onClick={freeze}>Hold chat-plan intent</button></div> : <div className="grid min-w-0 gap-3">
      <div className="break-all"><KeyValues items={[
        ['Original request', intent.source.requestId], ['Original automation', intent.source.automationId], ['Expected version', intent.source.versionId], ['Original graph digest', intent.source.graphDigest], ['Variable syntax', `Syntax ${intent.syntaxVersion}`], ['Tab plan state', metadata ? 'Prepared metadata verified' : intent.phase === 'prepared' ? 'Held; no preparation sent' : 'Submitted; check original preparation'],
      ]} /></div>
      <div className="flex flex-wrap gap-2"><button className="desk-btn desk-btn--primary" disabled={busy || blocked || !!storageError || intent.phase !== 'prepared'} onClick={() => void perform(submit)}>Prepare chat plan</button><button className="desk-btn" disabled={busy || intent.phase !== 'submitted'} onClick={() => void perform(check)}>Check original preparation</button><button className="desk-btn" disabled={busy} onClick={clear}>Acknowledge and clear chat-plan handle</button></div>
      <p className="text-sm">This handle stays bound to its original occurrence even if a newer rehearsal is loaded. Preparation is sent once. After a lost response, reload or error, only the read-only check is available; it never prepares again or creates a replacement request. Clearing changes tab bookkeeping only.</p>
    </div>}
    {metadata ? <ChatPlanSummary metadata={metadata} /> : null}
    <OperatorChatAdmissionDesk scope={scope} plan={intent} metadata={metadata} blocked={busy || blocked || !!storageError} />
  </section>;
}

export function ChatPlanSummary({ metadata }: { metadata: ChatPlanMetadata }) {
  return <section aria-label="Verified chat-plan metadata" className="grid min-w-0 gap-3">
    <Tag>Prepared metadata · no delivery queued</Tag>
    <div className="break-all"><KeyValues items={[
      ['Plan digest', metadata.digest], ['Replies', String(metadata.effectCount)], ['Channel', metadata.channelId], ['Connection', metadata.connectionId], ['Prepared at', metadata.preparedAt], ['Original deadline', metadata.deadline], ['Source', metadata.sourceId], ['Original run', metadata.runId], ['Version', metadata.versionId], ['Graph', metadata.graphDigest], ['Response', metadata.unchanged ? 'Existing preparation returned' : 'New preparation'],
    ]} /></div>
    <p className="text-sm">The private reply text stays on the server. This metadata does not authorize delivery; admission is a separate explicit step that still needs a ready sender and the original deadline.</p>
  </section>;
}
