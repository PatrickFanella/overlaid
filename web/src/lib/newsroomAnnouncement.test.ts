import { afterEach, describe, expect, it, vi } from 'vitest';
import { announceNewsroom, announcementStorageKey, clearAnnouncementIntent, hasAnnouncementIntent } from './newsroomAnnouncement';

const scope = {baseUrl:'http://example.test',tenantId:'workspace-a',actorId:'actor-a'};
const fingerprint='a'.repeat(64);
const preview={mode:'retained',tenantId:scope.tenantId,actorId:scope.actorId,fingerprint};
const key='00000000-0000-4000-8000-000000001234';
function storage() {
 const data=new Map<string,string>();
 return {getItem:(key:string)=>data.get(key)??null,setItem:(key:string,value:string)=>{data.set(key,value);},removeItem:(key:string)=>{data.delete(key);}};
}
function response(value:unknown) {return {ok:true,json:async()=>value};}
function status(state:string) {return {mode:'retained',state,queued:state==='pending'||state==='running',announced:state==='delivered',receipt:{sourceId:'b'.repeat(64),actionId:'c'.repeat(64),state,deadline:'2026-10-16T10:00:00Z'}};}
afterEach(()=>vi.unstubAllGlobals());

describe('manual announcement intents',()=>{
 it('saves one scoped intent before POST and distinguishes queued from delivered',async()=>{
  const saved=storage();
  const fetch=vi.fn().mockResolvedValueOnce(response(preview)).mockImplementationOnce(async(_url,options)=>{
   expect(hasAnnouncementIntent(scope,'current',saved)).toBe(true);
   const body=JSON.parse(options.body);expect(body).toMatchObject({mode:'retained',tenantId:scope.tenantId,actorId:scope.actorId,fingerprint,retry:false});expect(body.requestKey).toMatch(/^[0-9a-f-]{36}$/);
   return response(status('pending'));
  });vi.stubGlobal('fetch',fetch);
  const result=await announceNewsroom(scope,'current','',saved);
  expect(result.message).toContain('Delivery has not been confirmed');expect(result.unresolved).toBe(false);expect(hasAnnouncementIntent(scope,'current',saved)).toBe(false);expect(fetch).toHaveBeenCalledTimes(2);
 });
 it('keeps the same key after a lost response and only checks its receipt on the next click',async()=>{
  const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response(preview)).mockRejectedValueOnce(new Error('response lost')).mockResolvedValueOnce(response(status('delivered')));vi.stubGlobal('fetch',fetch);
  await expect(announceNewsroom(scope,'current','',saved)).rejects.toThrow('response lost');
  const intent=JSON.parse(saved.getItem(announcementStorageKey(scope,'current'))!);
  expect(intent.mode).toBe('retained');expect(Object.keys(intent).sort()).toEqual(['fingerprint','mode','requestKey','version']);
  expect(await announceNewsroom(scope,'current','',saved)).toEqual({message:'Posted in Twitch chat.',unresolved:false});
  expect(fetch).toHaveBeenCalledTimes(3);
  const first=JSON.parse(fetch.mock.calls[1][1].body);const retry=JSON.parse(fetch.mock.calls[2][1].body);
  expect(retry).toEqual({...first,retry:true});expect(hasAnnouncementIntent(scope,'current',saved)).toBe(false);
 });
 it('restores an unresolved intent after reload without preparing or admitting another source',async()=>{
  const saved=storage();saved.setItem(announcementStorageKey(scope,'raffle'),JSON.stringify({version:1,mode:'retained',requestKey:key,fingerprint}));
  const fetch=vi.fn().mockResolvedValue({ok:false,status:409,text:async()=> 'no receipt is available'});vi.stubGlobal('fetch',fetch);
  await expect(announceNewsroom(scope,'raffle','',saved)).rejects.toThrow('no receipt');expect(fetch).toHaveBeenCalledOnce();expect(fetch.mock.calls[0][1].method).toBe('POST');expect(JSON.parse(fetch.mock.calls[0][1].body).retry).toBe(true);expect(hasAnnouncementIntent(scope,'raffle',saved)).toBe(true);
 });
 it('keeps uncertain and malformed responses unresolved without automatic replay',async()=>{
  for(const value of [status('uncertain'),{...status('pending'),announced:true},{}]) {
   const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response(preview)).mockResolvedValueOnce(response(value));vi.stubGlobal('fetch',fetch);
   if('state' in value&&value.state==='uncertain') expect((await announceNewsroom(scope,'current','',saved)).unresolved).toBe(true);
   else await expect(announceNewsroom(scope,'current','',saved)).rejects.toThrow('could not be confirmed');
   expect(hasAnnouncementIntent(scope,'current',saved)).toBe(true);expect(fetch).toHaveBeenCalledTimes(2);
  }
 });
 it('distinguishes local allowance, provider quota and permissions on receipt-only retries',async()=>{
  for(const [failure,text] of [['plan_limited',"workspace's hourly HTTP allowance"],['quota_limited','Twitch rejected'],['unauthorized','could not be authorized'],['private-unknown-failure','The announcement was not delivered.']]) {
   const saved=storage();saved.setItem(announcementStorageKey(scope,'current'),JSON.stringify({version:1,mode:'retained',requestKey:key,fingerprint}));
   const value=status('rejected');const fetch=vi.fn().mockResolvedValue(response({...value,receipt:{...value.receipt,failure}}));vi.stubGlobal('fetch',fetch);
   const result=await announceNewsroom(scope,'current','',saved);
   expect(result.message).toContain(text);expect(result.message).not.toContain('private-unknown');expect(result.unresolved).toBe(false);expect(fetch).toHaveBeenCalledOnce();expect(JSON.parse(fetch.mock.calls[0][1].body).retry).toBe(true);expect(hasAnnouncementIntent(scope,'current',saved)).toBe(false);
  }
 });
 it('keeps malformed or contradictory failure metadata unresolved',async()=>{
  for(const value of [{...status('delivered'),receipt:{...status('delivered').receipt,failure:'plan_limited'}},{...status('rejected'),receipt:{...status('rejected').receipt,failure:{private:'body'}}}]) {
   const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response(preview)).mockResolvedValueOnce(response(value));vi.stubGlobal('fetch',fetch);
   await expect(announceNewsroom(scope,'current','',saved)).rejects.toThrow('could not be confirmed');expect(hasAnnouncementIntent(scope,'current',saved)).toBe(true);
  }
 });
 it('never retries direct delivery after ambiguity and clears only by explicit acknowledgement',async()=>{
  const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response({...preview,mode:'direct'})).mockRejectedValueOnce(new Error('network lost'));vi.stubGlobal('fetch',fetch);
  await expect(announceNewsroom(scope,'current','',saved)).rejects.toThrow('network lost');
  await expect(announceNewsroom(scope,'current','',saved)).rejects.toThrow('cannot be checked automatically');expect(fetch).toHaveBeenCalledTimes(2);
  clearAnnouncementIntent(scope,'current',saved);expect(hasAnnouncementIntent(scope,'current',saved)).toBe(false);
 });
 it('blocks POST when storage or preview scope cannot be trusted',async()=>{
  const saved=storage();const fetch=vi.fn().mockResolvedValue(response({...preview,tenantId:'workspace-b'}));vi.stubGlobal('fetch',fetch);
  await expect(announceNewsroom(scope,'current','',saved)).rejects.toThrow('account or workspace changed');expect(fetch).toHaveBeenCalledOnce();
  fetch.mockResolvedValue(response(preview));const unavailable={...saved,setItem:()=>{throw new Error('storage blocked');}};
  await expect(announceNewsroom(scope,'current','',unavailable)).rejects.toThrow('storage blocked');expect(fetch).toHaveBeenCalledTimes(2);
 });
 it('shares overlapping clicks and separates actor, workspace and endpoint storage',async()=>{
  const saved=storage();const fetch=vi.fn().mockResolvedValueOnce(response(preview)).mockResolvedValueOnce(response(status('pending')));vi.stubGlobal('fetch',fetch);
  const first=announceNewsroom(scope,'current','',saved);const second=announceNewsroom(scope,'current','',saved);expect(first).toBe(second);await first;expect(fetch).toHaveBeenCalledTimes(2);
  expect(new Set([announcementStorageKey(scope,'current'),announcementStorageKey({...scope,actorId:'actor-b'},'current'),announcementStorageKey({...scope,tenantId:'workspace-b'},'current'),announcementStorageKey({...scope,baseUrl:'http://other.test'},'current')]).size).toBe(4);
 });
});
