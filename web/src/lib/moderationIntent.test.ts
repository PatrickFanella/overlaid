import directReceiptFixture from '../../../backend/internal/http/testdata/direct-moderation-receipts.json?raw';
import { afterEach, expect, it, vi } from 'vitest';
import { clearModerationIntent, hasModerationIntent, moderationIntentKey, runModerationIntent } from './moderationIntent';
const scope = {baseUrl:'http://example.test',tenantId:'workspace-a',actorId:'actor-a'};
const preview = {...scope,mode:'retained',connectionId:'twitch:123'};
const payload = {action:'timeout' as const,targetUserId:'777',durationSeconds:60,reason:'PRIVATE_REASON'};
function storage() {const map=new Map<string,string>();return {getItem:(key:string)=>map.get(key)??null,setItem:(key:string,value:string)=>{map.set(key,value);},removeItem:(key:string)=>{map.delete(key);}};}
function response(value:unknown) {return {ok:true,json:async()=>value};}
function status(state:string) {const sourceId='a'.repeat(64),actionId='b'.repeat(64);return {mode:'retained',state,queued:state==='pending'||state==='running',receipt:{sourceId,actionIds:[actionId],deadline:'2026-10-17T10:00:00Z',receipts:[{actionId,manualSourceId:sourceId,state,connectionId:'twitch:123',capability:'user.timeout',eventId:'',code:state}]}};}
afterEach(()=>vi.unstubAllGlobals());
it('saves before submitting and retains queued work until terminal confirmation',async()=>{
 const saved=storage();
 const fetch=vi.fn().mockResolvedValueOnce(response(preview)).mockImplementationOnce(async(_url,options)=>{expect(hasModerationIntent(scope,saved)).toBe(true);expect(JSON.parse(options.body)).toMatchObject({...payload,mode:'retained',tenantId:scope.tenantId,actorId:scope.actorId,retry:false});return response(status('pending'));}).mockResolvedValueOnce(response(status('delivered')));
 vi.stubGlobal('fetch',fetch);
 expect((await runModerationIntent(scope,payload,'',saved)).result).toBe('pending');expect(hasModerationIntent(scope,saved)).toBe(true);
 expect((await runModerationIntent(scope,{action:'ban',targetUserId:'999'},'',saved)).result).toBe('delivered');
 expect(JSON.parse(fetch.mock.calls[2][1].body)).toEqual({...JSON.parse(fetch.mock.calls[1][1].body),retry:true});expect(hasModerationIntent(scope,saved)).toBe(false);
});
it('response loss and reload only check the original receipt without re-admission',async()=>{
 const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response(preview)).mockRejectedValueOnce(new Error('response lost')).mockResolvedValueOnce(response(status('running')));vi.stubGlobal('fetch',fetch);
 await expect(runModerationIntent(scope,payload,'',saved)).rejects.toThrow('response lost');
 const original=saved.getItem(moderationIntentKey(scope));
 expect((await runModerationIntent(scope,payload,'',saved)).result).toBe('running');expect(saved.getItem(moderationIntentKey(scope))).toBe(original);expect(fetch).toHaveBeenCalledTimes(3);expect(JSON.parse(fetch.mock.calls[2][1].body).retry).toBe(true);
});
it('never reconstructs missing receipts or clears uncertain and malformed results',async()=>{
 for(const result of [response(status('uncertain')),response({...status('pending'),queued:false}),response({...status('delivered'),state:['delivered']}),response({...status('delivered'),receipt:{...status('delivered').receipt,sourceId:['a'.repeat(64)]}}),response({...status('delivered'),receipt:{...status('delivered').receipt,receipts:[]}}),{ok:false,status:409,text:async()=>'no receipt'}]) {
  const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response(preview)).mockResolvedValueOnce(result);vi.stubGlobal('fetch',fetch);
  try {await runModerationIntent(scope,payload,'',saved);} catch { /* Remains unresolved. */ }
  expect(hasModerationIntent(scope,saved)).toBe(true);expect(fetch).toHaveBeenCalledTimes(2);
 }
});
it('requires durable browser storage before any submit and isolates accounts/workspaces',async()=>{
 const saved=storage();const fetch=vi.fn().mockResolvedValue(response(preview));vi.stubGlobal('fetch',fetch);
 await expect(runModerationIntent(scope,payload,'',{...saved,setItem:()=>{throw new Error('storage denied');}})).rejects.toThrow('storage denied');expect(fetch).toHaveBeenCalledOnce();
 expect(moderationIntentKey({...scope,actorId:'b'})).not.toBe(moderationIntentKey(scope));expect(moderationIntentKey({...scope,tenantId:'b'})).not.toBe(moderationIntentKey(scope));
 await expect(runModerationIntent({...scope,actorId:'changed'},payload,'',saved)).rejects.toThrow('account, workspace or channel changed');expect(hasModerationIntent(scope,saved)).toBe(false);
});
it('direct ambiguity cannot replay and explicit acknowledgment is required',async()=>{
 const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response({...preview,mode:'direct'})).mockRejectedValueOnce(new Error('lost'));vi.stubGlobal('fetch',fetch);
 await expect(runModerationIntent(scope,payload,'',saved)).rejects.toThrow('lost');await expect(runModerationIntent(scope,payload,'',saved)).rejects.toThrow('cannot be checked automatically');expect(fetch).toHaveBeenCalledTimes(2);
 clearModerationIntent(scope,saved);expect(hasModerationIntent(scope,saved)).toBe(false);
});
it('deduplicates concurrent clicks and preserves contradictory failures',async()=>{
 const saved=storage();const value=status('delivered');const fetch=vi.fn().mockResolvedValueOnce(response(preview)).mockResolvedValueOnce(response({...value,receipt:{...value.receipt,receipts:[{...value.receipt.receipts[0],failure:'unauthorized'}]}}));vi.stubGlobal('fetch',fetch);
 const first=runModerationIntent(scope,payload,'',saved),second=runModerationIntent(scope,payload,'',saved);expect(first).toBe(second);await expect(first).rejects.toThrow('could not be confirmed');expect(hasModerationIntent(scope,saved)).toBe(true);
});

it('recognizes actual direct HTTP receipts for all four actions and clears only confirmed work',async()=>{
 const receipts=JSON.parse(directReceiptFixture) as Record<string,unknown>;
 for(const action of ['delete','timeout','ban','unban'] as const) {
  const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response({...preview,mode:'direct'})).mockResolvedValueOnce(response(receipts[action]));vi.stubGlobal('fetch',fetch);
  const result=await runModerationIntent(scope,{...payload,action},'',saved);
  expect(result.result).toBe('delivered');expect(result.action).toBe(action);expect(hasModerationIntent(scope,saved)).toBe(false);expect(fetch).toHaveBeenCalledTimes(2);
 }
});
it('does not interpret audit labels, failed, queued, or wrong-action direct responses as completion',async()=>{
 for(const value of [{action:'timeout',result:'allowed'},{action:'timeout',result:'failed'},{action:'timeout',result:'pending'},status('pending'),{action:'ban',result:'accepted',reason:'twitch_helix'},{action:'timeout',result:'accepted',reason:'queued'}]) {
  const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response({...preview,mode:'direct'})).mockResolvedValueOnce(response(value));vi.stubGlobal('fetch',fetch);
  await expect(runModerationIntent(scope,payload,'',saved)).rejects.toThrow('could not be confirmed');expect(hasModerationIntent(scope,saved)).toBe(true);
 }
});
