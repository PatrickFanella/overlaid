import receiptFixture from '../../../backend/internal/http/testdata/retained-nuke-receipts.json?raw';
import { afterEach, expect, it, vi } from 'vitest';
import { canSubmitNuke, clearNukeIntent, loadNukeIntent, nukeIntentKey, previewNuke, submitNuke } from './nukeIntent';
const scope = {baseUrl:'http://example.test',tenantId:'workspace-a',actorId:'actor-a'};
const projection = {mode:'retained',tenantId:scope.tenantId,actorId:scope.actorId,connectionId:'twitch:123'};
const draft = {pattern:'spam',regex:false,caseSensitive:false,lookbackMinutes:30,limit:100,action:'timeout' as const,durationSeconds:90,radiationMinutes:0};
const target = {eventId:'event-1',messageId:'msg-1',userId:'777',user:'viewer',message:'spam',createdAt:'2026-10-19T10:00:00Z'};
function storage() {const map=new Map<string,string>();return {getItem:(k:string)=>map.get(k)??null,setItem:(k:string,v:string)=>{map.set(k,v);},removeItem:(k:string)=>{map.delete(k);}};}
function response(value:unknown) {return {ok:true,json:async()=>value};}
function preview(radiation=0,empty=false) {const now=Date.now();return {...projection,options:{...draft,limit:undefined,historyLimit:100,radiationMinutes:radiation},targets:empty?[]:[target],observedAt:new Date(now).toISOString(),expiresAt:new Date(now+120000).toISOString(),plan:empty&&!radiation?'':'sealed-private-plan',fingerprint:empty&&!radiation?'':'c'.repeat(64)};}
function status(states=['pending'],radiation=false) {const sourceId='a'.repeat(64),ids=states.map((_,i)=>(i+1).toString().repeat(64));return {mode:'retained',queued:states.some(s=>s==='pending'||s==='running'),radiationOnly:states.length===0,receipt:{sourceId,actionIds:ids,created:true,createdAt:'2026-10-19T10:00:00Z',deadline:'2026-10-19T10:02:00Z',...(radiation?{radiationRuleId:'d'.repeat(64)}:{}),receipts:states.map((state,i)=>({actionId:ids[i],manualSourceId:sourceId,eventId:'',connectionId:'twitch:123',capability:'user.timeout',state,code:state}))}};}
afterEach(()=>vi.unstubAllGlobals());
it('persists the exact preview, then one request UUID before POST; reload checks only',async()=>{
 const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response(projection)).mockResolvedValueOnce(response(preview())).mockImplementationOnce(async(_url,options)=>{expect(loadNukeIntent(scope,saved)?.requestKey).toBeTruthy();expect(JSON.parse(options.body)).toMatchObject({plan:'sealed-private-plan',retry:false});throw new Error('lost response');}).mockResolvedValueOnce(response(status(['delivered'])));vi.stubGlobal('fetch',fetch);
 await previewNuke(scope,draft,'',saved);expect(loadNukeIntent(scope,saved)?.display.matches).toEqual([target]);
 await expect(submitNuke(scope,'',saved)).rejects.toThrow('lost response');const original=saved.getItem(nukeIntentKey(scope));
 expect((await submitNuke(scope,'',saved)).states).toEqual(['delivered']);expect(saved.getItem(nukeIntentKey(scope))).toBe(original);
 expect(JSON.parse(fetch.mock.calls[3][1].body)).toEqual({...JSON.parse(fetch.mock.calls[2][1].body),retry:true});
 await expect(previewNuke(scope,draft,'',saved)).rejects.toThrow('saved nuke request');expect(fetch).toHaveBeenCalledTimes(4);
});
it('retains pending, uncertain, malformed, missing and contradictory outcomes',async()=>{
 const contradictory=status(['delivered']);contradictory.receipt.receipts[0].connectionId='twitch:999';
 for(const value of [response(status()),response(status(['uncertain'])),response({...status(['delivered']),queued:true}),response({...status(['delivered']),receipt:{...status().receipt,receipts:[]}}),response(contradictory),{ok:false,status:409,text:async()=>'receipt missing'}]) {
  const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response(projection)).mockResolvedValueOnce(response(preview())).mockResolvedValueOnce(value);vi.stubGlobal('fetch',fetch);
  await previewNuke(scope,draft,'',saved);try{await submitNuke(scope,'',saved);}catch{/* Intentionally unresolved. */}
  expect(loadNukeIntent(scope,saved)?.requestKey).toBeTruthy();expect(canSubmitNuke(loadNukeIntent(scope,saved))).toBe(false);
 }
});
it('allows zero-target radiation configuration and refuses an empty effect-free preview',async()=>{
 const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response(projection)).mockResolvedValueOnce(response(preview(5,true))).mockResolvedValueOnce(response(status([],true)));vi.stubGlobal('fetch',fetch);
 expect(canSubmitNuke(await previewNuke(scope,{...draft,radiationMinutes:5},'',saved))).toBe(true);
 expect(await submitNuke(scope,'',saved)).toMatchObject({radiationOnly:true,queued:false,states:[]});expect(loadNukeIntent(scope,saved)?.requestKey).toBeTruthy();
 clearNukeIntent(scope,saved);fetch.mockResolvedValueOnce(response(projection)).mockResolvedValueOnce(response(preview(0,true)));
 expect(canSubmitNuke(await previewNuke(scope,draft,'',saved))).toBe(false);await expect(submitNuke(scope,'',saved)).rejects.toThrow('no admissible');
});
it('uses original expiry and refuses unavailable storage before any effect POST',async()=>{
 const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response(projection)).mockResolvedValueOnce(response(preview()));vi.stubGlobal('fetch',fetch);await previewNuke(scope,draft,'',saved);
 const p=loadNukeIntent(scope,saved)!;p.expiresAt='2020-01-01T00:00:00Z';saved.setItem(nukeIntentKey(scope),JSON.stringify(p));await expect(submitNuke(scope,'',saved)).rejects.toThrow('expired');expect(fetch).toHaveBeenCalledTimes(2);
 p.expiresAt=new Date(Date.now()+120000).toISOString();saved.setItem(nukeIntentKey(scope),JSON.stringify(p));
 await expect(submitNuke(scope,'',{...saved,setItem:()=>{throw new Error('storage denied');}})).rejects.toThrow('storage denied');expect(fetch).toHaveBeenCalledTimes(2);
 expect(loadNukeIntent({...scope,actorId:'other'},saved)).toBeNull();expect(loadNukeIntent({...scope,tenantId:'other'},saved)).toBeNull();
});
it('deduplicates concurrent submissions and never falls back after retained failure or mode drift',async()=>{
 const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response(projection)).mockResolvedValueOnce(response(preview())).mockResolvedValueOnce({ok:false,status:409,text:async()=>'mode changed'});vi.stubGlobal('fetch',fetch);await previewNuke(scope,draft,'',saved);
 const a=submitNuke(scope,'',saved),b=submitNuke(scope,'',saved);expect(a).toBe(b);await expect(a).rejects.toThrow('mode changed');expect(fetch).toHaveBeenCalledTimes(3);expect(fetch.mock.calls.every(([url])=>!String(url).endsWith('/execute'))).toBe(true);
});
it('direct ambiguity survives reload and cannot replay automatically',async()=>{
 const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response({...projection,mode:'direct'})).mockResolvedValueOnce(response({matches:[target],pattern:'spam',action:'timeout'})).mockRejectedValueOnce(new Error('lost'));vi.stubGlobal('fetch',fetch);await previewNuke(scope,draft,'',saved);
 await expect(submitNuke(scope,'',saved)).rejects.toThrow('lost');await expect(submitNuke(scope,'',saved)).rejects.toThrow('cannot be checked');expect(fetch).toHaveBeenCalledTimes(3);
});

it('reads Go-serialized mixed and radiation-only receipt contracts without implying delivery',async()=>{
 const fixtures=JSON.parse(receiptFixture);
 for(const [name,receipt] of Object.entries(fixtures)) {
  const saved=storage();const radio=name==='radiation';const p=preview(radio?5:0,radio);
  if(!radio)p.targets=[0,1,2,3].map(i=>({...target,eventId:`event-${i}`,userId:`${i}`}));
  const fetch=vi.fn().mockResolvedValueOnce(response(projection)).mockResolvedValueOnce(response(p)).mockResolvedValueOnce(response(receipt));vi.stubGlobal('fetch',fetch);
  await previewNuke(scope,{...draft,radiationMinutes:radio?5:0},'',saved);
  const result=await submitNuke(scope,'',saved);
  expect(result.radiationOnly).toBe(radio);expect(result.queued).toBe(!radio);
  expect(result.states).toEqual(radio?[]:['delivered','rejected','uncertain','pending']);
  expect(loadNukeIntent(scope,saved)?.requestKey).toBeTruthy();
 }
});
