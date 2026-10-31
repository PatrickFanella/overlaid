import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { cancelChatAdmission, loadChatAdmissionIntent, recoverChatAdmission, submitChatAdmission, writeChatAdmissionIntent, type ChatAdmissionRequest } from './operatorChatAdmissionFlow';
import { decodeChatAdmissionIntent, decodeChatAdmissionReceipt, decodeChatAdmissionStatus, freezeChatAdmissionIntent, chatAdmissionStorageKey, type ChatAdmissionIntent } from './operatorChatAdmissions';
import type { ChatPlanStorage } from './operatorChatPlanFlow';
import type { ChatPlanIntent, ChatPlanMetadata } from './operatorChatPlans';
import { operatorIdentities, operatorIdentity, type OperatorRun } from './operatorRehearsal';
import { ChatAdmissionStatusSummary } from '../routes/dashboard/OperatorChatAdmissionDesk';

const scope = { accountId: '11111111-1111-4111-8111-111111111111', tenantId: '22222222-2222-4222-8222-222222222222', csrf: 'private', baseURL: 'http://example.test' };
const plan: ChatPlanIntent = { format: 1, phase: 'submitted', syntaxVersion: 2, source: { format: 1, phase: 'submitted', requestId: '66666666-6666-4666-8666-666666666666', automationId: '33333333-3333-4333-8333-333333333333', versionId: '44444444-4444-4444-8444-444444444444', documentDigest: 'a'.repeat(64), graphDigest: 'b'.repeat(64) } };
const ids = ['1'.repeat(64), '2'.repeat(64)];

async function fixture() {
  const identities = await operatorIdentities(scope, plan.source);
  const run: OperatorRun = { ...identities, automationId: plan.source.automationId, versionId: plan.source.versionId, graphDigest: plan.source.graphDigest, createdAt: '2026-10-30T12:00:00.123456789Z', deadline: '2026-10-30T12:02:00.123456789Z', unchanged: true, state: 'admitted_metadata' };
  const metadata: ChatPlanMetadata = { sourceId: await operatorIdentity(['overlaid.engine.operator-chat-source.v1', scope.tenantId, run.runId]), runId: run.runId, automationId: run.automationId, versionId: run.versionId, graphDigest: run.graphDigest, connectionId: 'twitch:123', channelId: '123', digest: 'c'.repeat(64), effectCount: 2, preparedAt: '2026-10-30T12:00:01.000000001Z', deadline: run.deadline, unchanged: true, state: 'prepared_metadata' };
  const held = freezeChatAdmissionIntent(plan, metadata)!;
  const receipt = { sourceId: metadata.sourceId, actionIds: ids, deadline: run.deadline, admittedAt: '2026-10-30T12:00:02Z', created: true, state: 'admitted_queued' };
  const status = { sourceId: metadata.sourceId, runId: run.runId, digest: metadata.digest, deadline: run.deadline, admittedAt: receipt.admittedAt, actions: [{ actionId: ids[0], sequence: 2, state: 'delivered', code: 'delivered', failure: '', retryAfter: 0, notBefore: run.createdAt, deadline: run.deadline }, { actionId: ids[1], sequence: 5, state: 'rejected', code: 'rejected', failure: 'quota_limited', retryAfter: 30000000000, notBefore: '2026-10-30T12:00:05.123456789Z', deadline: '2026-10-30T12:02:05.123456789Z' }] };
  return { run, metadata, held, receipt, status };
}
function memory(initial: string | null = null, failWrites = false) {
  let value = initial;
  const storage: ChatPlanStorage = { get: () => value, set: raw => { if (failWrites) throw new Error('quota'); value = raw; }, remove: () => { value = null; } };
  return { storage, read: () => value };
}
function server(responses: Record<string, () => unknown>, read: () => string | null) {
  const calls: { operation: string; body: Record<string, unknown>; stored: string | null }[] = [];
  const request: ChatAdmissionRequest = async (operation, body) => { calls.push({ operation, body, stored: read() }); const r = responses[operation]; if (!r) throw new Error(`unexpected ${operation}`); return r(); };
  return { calls, request };
}
const live = () => undefined;
const signal = new AbortController().signal;
const phase = (raw: string | null) => (JSON.parse(raw!) as ChatAdmissionIntent).phase;

describe('chat delivery admission flow', () => {
  it('holds only verified prepared metadata for the same plan and round trips the handle', async () => {
    const { metadata, held } = await fixture();
    expect(held).toMatchObject({ phase: 'prepared', digest: metadata.digest, effectCount: 2, sourceId: metadata.sourceId });
    expect(freezeChatAdmissionIntent({ ...plan, phase: 'prepared' }, metadata)).toBeNull();
    expect(freezeChatAdmissionIntent(plan, null)).toBeNull();
    expect(freezeChatAdmissionIntent(plan, { ...metadata, versionId: scope.accountId })).toBeNull();
    expect(decodeChatAdmissionIntent(JSON.stringify(held))).toEqual(held);
    for (const bad of ['{', 'x'.repeat(4097), JSON.stringify({ ...held, phase: 'retry' }), JSON.stringify({ ...held, digest: 'bad' }), JSON.stringify({ ...held, effectCount: 17 }), JSON.stringify({ ...held, preparedAt: held.deadline }), JSON.stringify({ ...held, plan: { ...plan, phase: 'prepared' } })]) expect(() => decodeChatAdmissionIntent(bad)).toThrow('unreadable');
    expect(chatAdmissionStorageKey({ ...scope, csrf: 'rotated' })).toBe(chatAdmissionStorageKey(scope));
    expect(chatAdmissionStorageKey(scope)).not.toBe(chatAdmissionStorageKey({ ...scope, tenantId: scope.accountId }));
  });

  it('re-reads the plan, stores submitted, then sends one admission with only request and digest', async () => {
    const { run, metadata, held, receipt } = await fixture();
    const m = memory(JSON.stringify(held));
    const s = server({ lookup: () => run, 'lookup-chat-plan': () => metadata, 'admit-chat-plan': () => ({ ...receipt, text: 'PRIVATE' }) }, m.read);
    const result = await submitChatAdmission({ scope, intent: held, storage: m.storage, request: s.request, signal, active: live });
    expect(s.calls.map(c => c.operation)).toEqual(['lookup', 'lookup-chat-plan', 'admit-chat-plan']);
    expect(s.calls[2].body).toEqual({ requestId: plan.source.requestId, digest: metadata.digest });
    expect(phase(s.calls[1].stored)).toBe('prepared');
    expect(phase(s.calls[2].stored)).toBe('submitted');
    expect(result.receipt).toEqual({ ...receipt, state: 'admitted_queued' });
    expect(JSON.stringify(result)).not.toContain('PRIVATE');
  });

  it('sends nothing when the plan changed or storage fails', async () => {
    const { run, metadata, held } = await fixture();
    const changed = memory(JSON.stringify(held));
    const s = server({ lookup: () => run, 'lookup-chat-plan': () => ({ ...metadata, digest: 'd'.repeat(64) }) }, changed.read);
    await expect(submitChatAdmission({ scope, intent: held, storage: changed.storage, request: s.request, signal, active: live })).rejects.toThrow('no longer matches');
    expect(phase(changed.read())).toBe('prepared');
    const broken = memory(JSON.stringify(held), true);
    const b = server({ lookup: () => run, 'lookup-chat-plan': () => metadata }, broken.read);
    await expect(submitChatAdmission({ scope, intent: held, storage: broken.storage, request: b.request, signal, active: live })).rejects.toThrow('not queued');
    for (const calls of [s.calls, b.calls]) expect(calls.map(c => c.operation)).not.toContain('admit-chat-plan');
    expect(writeChatAdmissionIntent(broken.storage, held)).toBe(false);
  });

  it('after a lost or malformed response allows only read-only outcome lookup, including after reload', async () => {
    const { run, metadata, held, receipt, status } = await fixture();
    const m = memory(JSON.stringify(held));
    const lost = server({ lookup: () => run, 'lookup-chat-plan': () => metadata, 'admit-chat-plan': () => ({ ...receipt, state: 'delivered' }) }, m.read);
    await expect(submitChatAdmission({ scope, intent: held, storage: m.storage, request: lost.request, signal, active: live })).rejects.toThrow('Invalid chat delivery');
    const reloaded = loadChatAdmissionIntent(m.storage).intent!;
    expect(reloaded.phase).toBe('submitted');
    await expect(submitChatAdmission({ scope, intent: reloaded, storage: m.storage, request: lost.request, signal, active: live })).rejects.toThrow('already submitted');
    expect(lost.calls.filter(c => c.operation === 'admit-chat-plan')).toHaveLength(1);
    const read = server({ 'lookup-chat-admission': () => status }, m.read);
    const result = await recoverChatAdmission({ scope, intent: reloaded, storage: m.storage, request: read.request, signal, active: live }, null);
    expect(result.actions.map(a => [a.state, a.failure, a.retryAfterNs, a.delayed])).toEqual([['delivered', '', 0, false], ['rejected', 'quota_limited', 30000000000, true]]);
    expect(read.calls).toEqual([{ operation: 'lookup-chat-admission', body: { requestId: plan.source.requestId, digest: metadata.digest }, stored: m.read() }]);
    const missing = server({ 'lookup-chat-admission': () => { throw new Error('automation_not_found'); } }, m.read);
    await expect(recoverChatAdmission({ scope, intent: reloaded, storage: m.storage, request: missing.request, signal, active: live }, null)).rejects.toThrow('automation_not_found');
    expect(phase(m.read())).toBe('submitted');
  });

  it('discards a late admission response after scope change but keeps the submitted handle', async () => {
    const { run, metadata, held, receipt } = await fixture();
    const m = memory(JSON.stringify(held));
    let current = true;
    const active = () => { if (!current) throw new Error('Delivery request canceled.'); };
    const s = server({ lookup: () => run, 'lookup-chat-plan': () => metadata, 'admit-chat-plan': () => { current = false; return receipt; } }, m.read);
    await expect(submitChatAdmission({ scope, intent: held, storage: m.storage, request: s.request, signal, active })).rejects.toThrow('canceled');
    expect(phase(m.read())).toBe('submitted');
  });

  it('rejects receipts and outcomes that contradict the original admission', async () => {
    const { held, receipt, status } = await fixture();
    const submitted = { ...held, phase: 'submitted' as const };
    for (const change of [{ sourceId: 'd'.repeat(64) }, { actionIds: [ids[0]] }, { actionIds: [ids[0], ids[0]] }, { actionIds: [ids[0], 'bad'] }, { created: 1 }, { deadline: '2026-10-30T12:02:00.123456788Z' }, { admittedAt: held.deadline }, { admittedAt: '2026-10-30T12:00:01Z' }]) expect(() => decodeChatAdmissionReceipt({ ...receipt, ...change }, submitted)).toThrow();
    const good = decodeChatAdmissionReceipt(receipt, submitted);
    expect(decodeChatAdmissionStatus(status, submitted, good).actions).toHaveLength(2);
    expect(() => decodeChatAdmissionStatus(status, submitted, { ...good, actionIds: [ids[1], ids[0]] })).toThrow();
    expect(() => decodeChatAdmissionStatus({ ...status, admittedAt: '2026-10-30T12:00:03Z' }, submitted, good)).toThrow();
    const action = status.actions[0];
    for (const bad of [{ state: 'pending', code: 'delivered' }, { state: 'delivered', code: 'delivered', failure: 'permanent' }, { state: 'rejected', code: 'expired', failure: 'retryable' }, { state: 'rejected', code: 'rejected', failure: 'plan_limited', retryAfter: 5 }, { state: 'rejected', code: 'rejected', failure: 'private_error' }, { state: 'uncertain', code: 'uncertain', retryAfter: 1 }, { retryAfter: -1 }, { retryAfter: 1.5 }, { sequence: 0 }, { state: 'sent', code: 'sent' }, { notBefore: undefined }, { deadline: '2026-10-30T12:02:00.123456788Z' }, { notBefore: '2026-10-30T12:02:00.123456789Z' }, { deadline: '2026-10-31T12:02:00.123456790Z', notBefore: '2026-10-31T12:00:00.123456790Z' }]) expect(() => decodeChatAdmissionStatus({ ...status, actions: [{ ...action, ...bad }, status.actions[1]] }, submitted, null)).toThrow();
    expect(() => decodeChatAdmissionStatus({ ...status, actions: [status.actions[1], status.actions[0]] }, submitted, null)).toThrow();
    expect(() => decodeChatAdmissionStatus({ ...status, digest: 'd'.repeat(64) }, submitted, null)).toThrow();
    for (const ok of [{ state: 'pending', code: 'admitted' }, { state: 'running', code: 'started' }, { state: 'uncertain', code: 'deadline_elapsed' }, { state: 'rejected', code: 'connection_unavailable', failure: 'permanent' }, { state: 'rejected', code: 'rejected', failure: 'plan_limited' }]) expect(() => decodeChatAdmissionStatus({ ...status, actions: [{ ...action, failure: '', ...ok }, status.actions[1]] }, submitted, null)).not.toThrow();
  });

  it('cancels unclaimed replies once per click, then reads outcomes without queueing', async () => {
    const { held, status } = await fixture();
    const submitted = { ...held, phase: 'submitted' as const };
    const m = memory(JSON.stringify(submitted));
    const after = { ...status, actions: [status.actions[0], { ...status.actions[1], state: 'rejected', code: 'canceled', failure: '', retryAfter: 0 }] };
    const ok = { sourceId: held.sourceId, canceledActionIds: [ids[1]], canceledAt: '2026-10-30T12:00:03Z', state: 'canceled_unclaimed', text: 'PRIVATE' };
    const s = server({ 'cancel-chat-admission': () => ok, 'lookup-chat-admission': () => after }, m.read);
    const result = await cancelChatAdmission({ scope, intent: submitted, storage: m.storage, request: s.request, signal, active: live }, null);
    expect(s.calls.map(c => c.operation)).toEqual(['cancel-chat-admission', 'lookup-chat-admission']);
    expect(s.calls[0].body).toEqual({ requestId: plan.source.requestId, digest: held.digest });
    expect(result.canceled).toEqual({ sourceId: held.sourceId, canceledActionIds: [ids[1]], canceledAt: '2026-10-30T12:00:03Z', state: 'canceled_unclaimed' });
    expect(result.status.actions[1].code).toBe('canceled');
    expect(JSON.stringify(result)).not.toContain('PRIVATE');
    await expect(cancelChatAdmission({ scope, intent: held, storage: m.storage, request: s.request, signal, active: live }, null)).rejects.toThrow('nothing to cancel');
    for (const bad of [{ sourceId: 'd'.repeat(64) }, { state: 'canceled' }, { canceledActionIds: [ids[1], ids[1]] }, { canceledActionIds: ['bad'] }, { canceledActionIds: [ids[0], ids[1], '3'.repeat(64)] }, { canceledAt: 'never' }]) {
      const b = server({ 'cancel-chat-admission': () => ({ ...ok, ...bad }), 'lookup-chat-admission': () => after }, m.read);
      await expect(cancelChatAdmission({ scope, intent: submitted, storage: m.storage, request: b.request, signal, active: live }, null)).rejects.toThrow();
    }
    // A reported cancellation must match the outcome read back.
    const contradict = server({ 'cancel-chat-admission': () => ({ ...ok, canceledActionIds: [ids[0]] }), 'lookup-chat-admission': () => after }, m.read);
    await expect(cancelChatAdmission({ scope, intent: submitted, storage: m.storage, request: contradict.request, signal, active: live }, null)).rejects.toThrow('contradicts');
    expect(() => decodeChatAdmissionStatus({ ...after, actions: [status.actions[0], { ...after.actions[1], failure: 'permanent' }] }, submitted, null)).toThrow();
    expect(phase(m.read())).toBe('submitted');
  });

  it('reports corrupt or unavailable storage without inventing a handle', () => {
    expect(loadChatAdmissionIntent(memory('{').storage)).toEqual({ intent: null, error: expect.stringContaining('unreadable') });
    expect(loadChatAdmissionIntent({ get: () => { throw new Error('SecurityError'); }, set: () => undefined, remove: () => undefined })).toEqual({ intent: null, error: expect.stringContaining('unavailable') });
  });

  it('renders outcomes as escaped text without controls or retry permission', async () => {
    const { held, status } = await fixture();
    const decoded = decodeChatAdmissionStatus(status, { ...held, phase: 'submitted' }, null);
    const html = renderToStaticMarkup(<ChatAdmissionStatusSummary status={{ ...decoded, runId: '<b>x</b>' }} />);
    expect(html).toContain('Delivered'); expect(html).toContain('retry hint 30s (informational)'); expect(html).toContain('delayed: not before 2026-10-30T12:00:05.123456789Z'); expect(html.match(/delayed:/g)).toHaveLength(1); expect(html).not.toContain('<button'); expect(html).not.toContain('<b>x</b>');
  });
});
