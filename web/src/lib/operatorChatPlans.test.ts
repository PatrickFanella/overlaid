import { expect, it } from 'vitest';
import { decodeChatPlanIntent, decodeChatPlanMetadata, chatPlanStorageKey, type ChatPlanIntent } from './operatorChatPlans';
import { operatorIdentities, operatorIdentity, type OperatorRun } from './operatorRehearsal';
const scope = { accountId: '11111111-1111-4111-8111-111111111111', tenantId: '22222222-2222-4222-8222-222222222222', csrf: 'private', baseURL: 'http://example.test' };
const intent: ChatPlanIntent = { format: 1, phase: 'submitted', syntaxVersion: 2, source: { format: 1, phase: 'submitted', requestId: '66666666-6666-4666-8666-666666666666', automationId: '33333333-3333-4333-8333-333333333333', versionId: '44444444-4444-4444-8444-444444444444', documentDigest: 'a'.repeat(64), graphDigest: 'b'.repeat(64) } };
async function fixture() {
 const ids = await operatorIdentities(scope, intent.source);
 const run: OperatorRun = { ...ids, automationId: intent.source.automationId, versionId: intent.source.versionId, graphDigest: intent.source.graphDigest, createdAt: '2026-10-30T12:00:00.123456789Z', deadline: '2026-10-30T12:02:00.123456789Z', unchanged: true, state: 'admitted_metadata' };
 const plan = { sourceId: await operatorIdentity(['overlaid.engine.operator-chat-source.v1', scope.tenantId, run.runId]), runId: run.runId, automationId: run.automationId, versionId: run.versionId, graphDigest: run.graphDigest, connectionId: 'twitch:123', channelId: '123', digest: 'c'.repeat(64), effectCount: 1, preparedAt: '2026-10-30T12:02:00.123456788Z', deadline: run.deadline, unchanged: true, state: 'prepared_metadata' };
 return { run, plan };
}
it('round trips frozen source handles without replacing identities and isolates scope keys', () => {
 expect(chatPlanStorageKey({...scope,csrf:'rotated'})).toBe(chatPlanStorageKey(scope));
 expect(decodeChatPlanIntent(null)).toBeNull();
 expect(decodeChatPlanIntent(JSON.stringify(intent))).toEqual(intent);
 for (const change of [{accountId: scope.tenantId}, {tenantId: scope.accountId}, {baseURL:'https://other.test'}]) expect(chatPlanStorageKey({...scope,...change})).not.toBe(chatPlanStorageKey(scope));
 for (const bad of ['{', '{}', 'x'.repeat(4097), JSON.stringify({...intent,syntaxVersion:1}),JSON.stringify({...intent,phase:'retry'}),JSON.stringify({...intent,source:{...intent.source,phase:'prepared'}})]) expect(()=>decodeChatPlanIntent(bad)).toThrow();
});
it('binds metadata to the original account/run/source and preserves nanosecond deadlines', async () => {
 const {run,plan}=await fixture();
 expect(await decodeChatPlanMetadata({...plan,text:'PRIVATE'},scope,intent,run)).toEqual(plan);
 await expect(decodeChatPlanMetadata(plan,{...scope,accountId:scope.tenantId},intent,run)).rejects.toThrow();
 await expect(decodeChatPlanMetadata(plan,scope,{...intent,phase:'prepared'},run)).rejects.toThrow();
 for (const change of [{sourceId:'d'.repeat(64)},{runId:'d'.repeat(64)},{versionId:scope.accountId},{graphDigest:'d'.repeat(64)},{state:'delivered'},{digest:'bad'},{effectCount:0},{effectCount:17},{effectCount:1.5},{unchanged:1},{channelId:'456'},{connectionId:'twitch:456'},{preparedAt:run.deadline},{preparedAt:'2026-10-30T12:00:00.123456788Z'},{deadline:'2026-10-30T12:02:00.123456788Z'},{preparedAt:'2026-02-30T12:00:00Z'}]) await expect(decodeChatPlanMetadata({...plan,...change},scope,intent,run)).rejects.toThrow();
});
