import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { freezeChatPlanIntent, loadChatPlanIntent, recoverChatPlan, submitChatPlan, writeChatPlanIntent, type ChatPlanRequest, type ChatPlanStorage } from './operatorChatPlanFlow';
import type { ChatPlanIntent } from './operatorChatPlans';
import { operatorIdentities, operatorIdentity, type OperatorRun, type RehearsalIntent } from './operatorRehearsal';
import { ChatPlanSummary } from '../routes/dashboard/OperatorChatPlanDesk';

const scope = { accountId: '11111111-1111-4111-8111-111111111111', tenantId: '22222222-2222-4222-8222-222222222222', csrf: 'private', baseURL: 'http://example.test' };
const source: RehearsalIntent = { format: 1, phase: 'submitted', requestId: '66666666-6666-4666-8666-666666666666', automationId: '33333333-3333-4333-8333-333333333333', versionId: '44444444-4444-4444-8444-444444444444', documentDigest: 'a'.repeat(64), graphDigest: 'b'.repeat(64) };

async function fixture() {
  const ids = await operatorIdentities(scope, source);
  const run: OperatorRun = { ...ids, automationId: source.automationId, versionId: source.versionId, graphDigest: source.graphDigest, createdAt: '2026-10-30T12:00:00.123456789Z', deadline: '2026-10-30T12:02:00.123456789Z', unchanged: true, state: 'admitted_metadata' };
  const plan = { sourceId: await operatorIdentity(['overlaid.engine.operator-chat-source.v1', scope.tenantId, run.runId]), runId: run.runId, automationId: run.automationId, versionId: run.versionId, graphDigest: run.graphDigest, connectionId: 'twitch:123', channelId: '123', digest: 'c'.repeat(64), effectCount: 2, preparedAt: '2026-10-30T12:00:01Z', deadline: run.deadline, unchanged: false, state: 'prepared_metadata' };
  return { run, plan };
}
function memory(initial: string | null = null, failWrites = false) {
  let value = initial;
  const storage: ChatPlanStorage = { get: () => value, set: raw => { if (failWrites) throw new Error('quota'); value = raw; }, remove: () => { value = null; } };
  return { storage, read: () => value };
}
type Call = { operation: string; body: Record<string, unknown>; storedAtCall: string | null };
function server(responses: Record<string, () => unknown>, read: () => string | null) {
  const calls: Call[] = [];
  const request: ChatPlanRequest = async (operation, body) => { calls.push({ operation, body, storedAtCall: read() }); const r = responses[operation]; if (!r) throw new Error(`unexpected ${operation}`); return r(); };
  return { calls, request };
}
const live = () => undefined;
const signal = new AbortController().signal;
const held = (): ChatPlanIntent => ({ format: 1, phase: 'prepared', source, syntaxVersion: 2 });

describe('chat-plan preparation flow', () => {
  it('holds only an accepted submitted source and sends nothing', async () => {
    const { run } = await fixture();
    expect(freezeChatPlanIntent(source, run)).toEqual(held());
    expect(freezeChatPlanIntent(null, run)).toBeNull();
    expect(freezeChatPlanIntent(source, null)).toBeNull();
    expect(freezeChatPlanIntent({ ...source, phase: 'prepared' }, run)).toBeNull();
    expect(freezeChatPlanIntent(source, { ...run, versionId: scope.accountId })).toBeNull();
  });

  it('stores the submitted phase before the single preparation POST and sends original identity only', async () => {
    const { run, plan } = await fixture();
    const m = memory(JSON.stringify(held()));
    const s = server({ lookup: () => run, 'prepare-chat-plan': () => ({ ...plan, text: 'PRIVATE' }) }, m.read);
    const result = await submitChatPlan({ scope, intent: held(), storage: m.storage, request: s.request, signal, active: live });
    expect(result.metadata).toEqual(plan);
    expect(result.intent.phase).toBe('submitted');
    expect(s.calls.map(c => c.operation)).toEqual(['lookup', 'prepare-chat-plan']);
    expect(s.calls[1].body).toEqual({ requestId: source.requestId, expectedVersionId: source.versionId, syntaxVersion: 2 });
    expect(JSON.parse(s.calls[1].storedAtCall!).phase).toBe('submitted');
    expect(JSON.parse(s.calls[0].storedAtCall!).phase).toBe('prepared');
    expect(JSON.stringify(result)).not.toContain('PRIVATE');
  });

  it('leaves only read-only recovery after a lost response, then recovers after reload without re-preparing', async () => {
    const { run, plan } = await fixture();
    const m = memory(JSON.stringify(held()));
    const lost = server({ lookup: () => run, 'prepare-chat-plan': () => { throw new Error('network'); } }, m.read);
    await expect(submitChatPlan({ scope, intent: held(), storage: m.storage, request: lost.request, signal, active: live })).rejects.toThrow('network');
    const reloaded = loadChatPlanIntent(m.storage);
    expect(reloaded.intent?.phase).toBe('submitted');
    await expect(submitChatPlan({ scope, intent: reloaded.intent!, storage: m.storage, request: lost.request, signal, active: live })).rejects.toThrow('already submitted');
    expect(lost.calls.filter(c => c.operation === 'prepare-chat-plan')).toHaveLength(1);
    const recovery = server({ lookup: () => run, 'lookup-chat-plan': () => ({ ...plan, unchanged: true }) }, m.read);
    await expect(recoverChatPlan({ scope, intent: reloaded.intent!, storage: m.storage, request: recovery.request, signal, active: live })).resolves.toMatchObject({ digest: plan.digest, unchanged: true });
    expect(recovery.calls.map(c => c.operation)).toEqual(['lookup', 'lookup-chat-plan']);
    expect(recovery.calls[1].body).toEqual({ requestId: source.requestId });
  });

  it('keeps the submitted handle when a receipt is malformed or the plan is missing', async () => {
    const { run, plan } = await fixture();
    const m = memory(JSON.stringify(held()));
    const bad = server({ lookup: () => run, 'prepare-chat-plan': () => ({ ...plan, state: 'delivered' }) }, m.read);
    await expect(submitChatPlan({ scope, intent: held(), storage: m.storage, request: bad.request, signal, active: live })).rejects.toThrow('Invalid chat-plan metadata');
    const submitted = loadChatPlanIntent(m.storage).intent!;
    expect(submitted).toEqual({ ...held(), phase: 'submitted' });
    const missing = server({ lookup: () => run, 'lookup-chat-plan': () => { throw new Error('automation_not_found'); } }, m.read);
    await expect(recoverChatPlan({ scope, intent: submitted, storage: m.storage, request: missing.request, signal, active: live })).rejects.toThrow('automation_not_found');
    expect(missing.calls.map(c => c.operation)).not.toContain('prepare-chat-plan');
    expect(m.read()).toBe(JSON.stringify(submitted));
  });

  it('refuses to send when the submitted phase cannot be stored', async () => {
    const { run } = await fixture();
    const m = memory(JSON.stringify(held()), true);
    const s = server({ lookup: () => run }, m.read);
    await expect(submitChatPlan({ scope, intent: held(), storage: m.storage, request: s.request, signal, active: live })).rejects.toThrow('was not sent');
    expect(s.calls.map(c => c.operation)).toEqual(['lookup']);
    expect(writeChatPlanIntent(m.storage, held())).toBe(false);
  });

  it('does not send preparation when the original run lookup mismatches', async () => {
    const { run } = await fixture();
    const m = memory(JSON.stringify(held()));
    const s = server({ lookup: () => ({ ...run, runId: 'f'.repeat(64) }) }, m.read);
    await expect(submitChatPlan({ scope, intent: held(), storage: m.storage, request: s.request, signal, active: live })).rejects.toThrow();
    expect(s.calls.map(c => c.operation)).toEqual(['lookup']);
    expect(JSON.parse(m.read()!).phase).toBe('prepared');
  });

  it('discards late results after a scope change during async validation', async () => {
    const { run, plan } = await fixture();
    const m = memory(JSON.stringify(held()));
    let current = true;
    const active = () => { if (!current) throw new Error('Chat-plan request canceled.'); };
    const s = server({ lookup: () => run, 'prepare-chat-plan': () => { current = false; return plan; } }, m.read);
    await expect(submitChatPlan({ scope, intent: held(), storage: m.storage, request: s.request, signal, active })).rejects.toThrow('canceled');
    expect(JSON.parse(m.read()!).phase).toBe('submitted');
    current = true;
    const early = server({ lookup: () => { current = false; return run; } }, m.read);
    const fresh = memory(JSON.stringify(held()));
    await expect(submitChatPlan({ scope, intent: held(), storage: fresh.storage, request: early.request, signal, active })).rejects.toThrow('canceled');
    expect(early.calls.map(c => c.operation)).toEqual(['lookup']);
    expect(JSON.parse(fresh.read()!).phase).toBe('prepared');
  });

  it('reports corrupt or unavailable storage without inventing a handle', () => {
    expect(loadChatPlanIntent(memory('{').storage)).toEqual({ intent: null, error: expect.stringContaining('unreadable') });
    const throwing: ChatPlanStorage = { get: () => { throw new Error('SecurityError'); }, set: () => undefined, remove: () => undefined };
    expect(loadChatPlanIntent(throwing)).toEqual({ intent: null, error: expect.stringContaining('unavailable') });
    const m = memory(JSON.stringify(held()));
    expect(writeChatPlanIntent(m.storage, null)).toBe(true);
    expect(m.read()).toBeNull();
  });

  it('renders verified metadata as text without delivery controls', async () => {
    const { plan } = await fixture();
    const html = renderToStaticMarkup(<ChatPlanSummary metadata={{ ...plan, channelId: '<b>x</b>' } as never} />);
    expect(html).toContain('no delivery queued'); expect(html).not.toContain('<button'); expect(html).not.toContain('<b>x</b>');
  });
});
