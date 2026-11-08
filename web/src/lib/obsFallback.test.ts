import { describe, expect, it } from 'vitest';
import { decodeOBSCurrent, decodeOBSIntent, decodeOBSProposal, matchOBSOutcome, obsIntentKey, obsName, recoverOBSApproval, submitOBSApproval, type OBSApprovalIntent, type OBSProposal, type OBSRequest, type OBSStorage } from './obsFallback';
import type { VersionScope } from './automationVersions';

const scope: VersionScope = { accountId: '11111111-1111-4111-8111-111111111111', tenantId: '22222222-2222-4222-8222-222222222222', csrf: 'fixture-csrf', baseURL: 'http://example.test' };
const proposal: OBSProposal = { id: 'a'.repeat(64), versionId: '33333333-3333-4333-8333-333333333333', graphDigest: 'b'.repeat(64), observationId: 'c'.repeat(64), condition: 'missing', reason: 'input_missing', scene: 'Fallback', observedAt: '2026-11-08T00:00:00.000001Z', deadline: '2026-11-08T00:00:10.000001Z', state: 'pending', failure: '', requestId: '', approvedScene: '', startedAt: null, finishedAt: null, unchanged: false };
const requestId = '44444444-4444-4444-8444-444444444444';
function memory(): OBSStorage { let raw: string | null = null; return { get: () => raw, set: value => { raw = value; }, remove: () => { raw = null; } }; }
function completed(intent: OBSApprovalIntent): OBSProposal { return { ...intent.proposal, state: 'succeeded', requestId: intent.requestId, approvedScene: intent.scene, startedAt: '2026-11-08T00:00:01Z', finishedAt: '2026-11-08T00:00:02Z' }; }
function frozen(): OBSApprovalIntent { return { format: 1, accountId: scope.accountId, tenantId: scope.tenantId, baseURL: scope.baseURL, requestId, scene: 'Override', proposal }; }

describe('OBS original approval flow', () => {
  it('persists the exact original scope/key/scene before one POST and prevents resubmission', async () => {
    const storage = memory(); const calls: string[] = [];
    const request: OBSRequest = async (operation, body) => {
      calls.push(operation);
      const saved = decodeOBSIntent(storage.get(), scope)!;
      expect(body).toEqual({ proposalId: proposal.id, versionId: proposal.versionId, requestId, scene: 'Override' });
      expect(saved.scene).toBe('Override');
      return { receipt: completed(saved) };
    };
    const result = await submitOBSApproval(scope, proposal, 'Override', storage, request, new AbortController().signal, () => {}, () => requestId);
    expect(result.state).toBe('succeeded');
    await expect(submitOBSApproval(scope, proposal, 'Other', storage, request, new AbortController().signal, () => {}, () => { throw new Error('replacement key'); })).rejects.toThrow('original approval');
    expect(calls).toEqual(['approve']);
  });
  it('keeps lost/malformed outcomes for read-only original lookup after reload', async () => {
    for (const malformed of [false, true]) {
      const storage = memory(); const calls: string[] = [];
      await expect(submitOBSApproval(scope, proposal, 'Override', storage, async op => { calls.push(op); if (malformed) return { receipt: { ...completed(frozen()), requestId: scope.accountId } }; throw new Error('lost response'); }, new AbortController().signal, () => {}, () => requestId)).rejects.toThrow();
      const restored = decodeOBSIntent(storage.get(), scope)!;
      const result = await recoverOBSApproval(restored, async (op, body) => { calls.push(op); expect(body).toEqual({ proposalId: proposal.id }); return { receipt: proposal }; }, new AbortController().signal, () => {});
      expect(result.state).toBe('pending'); expect(storage.get()).not.toBeNull(); expect(calls).toEqual(['approve', 'lookup']);
    }
  });
  it('sends nothing when storage fails and retains the handle if scope changes after persistence', async () => {
    let calls = 0;
    const request: OBSRequest = async () => { calls++; return {}; };
    const unavailable: OBSStorage = { get: () => null, set: () => { throw new Error('quota'); }, remove: () => {} };
    await expect(submitOBSApproval(scope, proposal, 'Override', unavailable, request, new AbortController().signal, () => {}, () => requestId)).rejects.toThrow();
    const storage = memory(); let checks = 0;
    await expect(submitOBSApproval(scope, proposal, 'Override', storage, request, new AbortController().signal, () => { if (++checks === 2) throw new Error('scope changed'); }, () => requestId)).rejects.toThrow('scope changed');
    expect(calls).toBe(0); expect(decodeOBSIntent(storage.get(), scope)?.requestId).toBe(requestId);
  });
  it('rejects cross-scope state and separates workspace/account/backend keys', () => {
    const raw = JSON.stringify(frozen());
    for (const other of [{ ...scope, accountId: requestId }, { ...scope, tenantId: requestId }, { ...scope, baseURL: 'https://other.test' }]) {
      expect(() => decodeOBSIntent(raw, other)).toThrow(); expect(obsIntentKey(other)).not.toBe(obsIntentKey(scope));
    }
  });
});

describe('OBS bounded metadata validation', () => {
  it('preserves sub-millisecond database deadlines and completion ordering', () => {
    const edge = { ...completed(frozen()), startedAt: '2026-11-08T00:00:10.000000Z', finishedAt: '2026-11-08T00:00:10.000002Z' };
    expect(decodeOBSProposal({ receipt: edge }).state).toBe('succeeded');
    expect(() => decodeOBSProposal({ receipt: { ...edge, startedAt: proposal.deadline } })).toThrow();
    expect(() => decodeOBSProposal({ receipt: { ...proposal, deadline: '2026-11-08T00:00:10.000000Z' } })).toThrow();
    expect(() => decodeOBSProposal({ receipt: { ...proposal, state: 'dismissed', failure: 'dismissed', finishedAt: '2026-11-08T00:00:00.000000Z' } })).toThrow();
  });
  it('refuses changed original identity, selected scene and contradictory completion fields', () => {
    const good = completed(frozen());
    expect(matchOBSOutcome({ receipt: good }, frozen()).state).toBe('succeeded');
    for (const bad of [
      { ...good, id: 'd'.repeat(64) }, { ...good, versionId: requestId }, { ...good, graphDigest: 'd'.repeat(64) }, { ...good, observationId: 'd'.repeat(64) }, { ...good, approvedScene: 'Other' },
      { ...good, deadline: '2026-11-08T00:01:10Z' }, { ...good, observedAt: '2026-02-30T00:00:00Z' }, { ...good, startedAt: good.deadline }, { ...good, finishedAt: null },
      { ...good, state: 'uncertain', failure: 'PRIVATE_PROVIDER_COMMENT' }, { ...good, failure: 'obs_unavailable' }, { ...proposal, state: 'dismissed', failure: 'dismissed', finishedAt: '2026-11-07T23:59:59Z' },
    ]) expect(() => matchOBSOutcome({ receipt: bad }, frozen())).toThrow();
    expect(() => decodeOBSProposal({ receipt: { ...proposal, requestId, approvedScene: 'Override' } })).toThrow();
  });
  it('accepts fixed terminal non-start and uncertain receipts without treating them as delivery', () => {
    expect(matchOBSOutcome({ receipt: { ...proposal, state: 'dismissed', failure: 'dismissed', finishedAt: '2026-11-08T00:00:02Z' } }, frozen()).state).toBe('dismissed');
    expect(matchOBSOutcome({ receipt: { ...completed(frozen()), state: 'uncertain', failure: 'obs_unavailable' } }, frozen()).state).toBe('uncertain');
  });
  it('validates rule names, observation states and bounds', () => {
    const rule = { automationId: scope.accountId, versionId: proposal.versionId, digest: proposal.graphDigest, revision: 1, collection: 'Collection', input: 'Input', scene: 'Fallback', enabled: true, latched: false, activeProposalId: proposal.id, updatedAt: proposal.observedAt, latest: { condition: 'muted', observedAt: proposal.observedAt, targetAvailable: true } };
    expect(decodeOBSCurrent({ available: true, rule }).rule?.latest?.condition).toBe('muted');
    for (const bad of [{ ...rule, revision: 0 }, { ...rule, scene: 'x'.repeat(256) }, { ...rule, latest: { ...rule.latest, condition: 'guess' } }, { ...rule, activeProposalId: 'private' }]) expect(() => decodeOBSCurrent({ available: true, rule: bad })).toThrow();
    expect(obsName('💬'.repeat(64))).toBe(false); expect(obsName('x\0y')).toBe(false);
  });
});
