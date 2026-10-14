import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import App from '../App';
import { NewsroomBundleCard } from '../routes/dashboard/NewsroomBundles';
import { demoNewsroom, orderedNewsroomItems, showNotes, shelfExport, visibleNewsroomRoster, activeNewsroomClaim, newsroomRecordingLink, reconcileNewsroomCardDraft, newsroomEvidenceLabel } from './newsroom';

describe('Newsroom workflow', () => {
  it('orders segment blocks without dropping ungrouped or stale-segment links', () => {
    const doc = demoNewsroom();
    doc.items.push({ ...doc.items[0], id: 'second', segmentId: 'segment' });
    doc.segments = [{ id: 'segment', name: 'Deep dive' }]; doc.order = ['community', 'second'];
    expect(orderedNewsroomItems(doc).map((i) => i.id)).toEqual(['second', 'community']);
    doc.segments = [];
    expect(orderedNewsroomItems(doc).map((i) => i.id)).toEqual(['community', 'second']);
  });
  it('exports only played links from the selected session, in playback order', () => {
    const doc = demoNewsroom();
    doc.items[0] = { ...doc.items[0], status: 'played', sessionId: 'a', notes: 'Takeaway', playedAt: '2026-10-13T19:01:00Z' };
    doc.items.push({ ...doc.items[0], id: 'other', title: 'Other session', sessionId: 'b' });
    const markdown = showNotes(doc, 'a');
    expect(markdown).toContain('Takeaway'); expect(markdown).not.toContain('Other session'); expect(markdown).not.toContain('transit');
  });
  it('renders the dashboard and OBS card through the application router', () => {
    expect(renderToStaticMarkup(<App pathname="/dashboard/newsroom" />)).toContain('Moderator queue');
    expect(renderToStaticMarkup(<App pathname="/overlay/newsroom" />)).toContain('Newsroom card');
    expect(renderToStaticMarkup(<App pathname="/newsroom/shared" />)).toContain('Shared shelf');
  });
});

describe('Newsroom retained editorial state', () => {
 it('uses retained history after queue items have been removed and excludes undo records', () => {
  const doc=demoNewsroom();doc.items=[];doc.history=[{id:'note',itemId:'removed',title:'Retained source',url:'https://example.org/source',notes:'Takeaway',at:'2026-10-13T19:00:00Z',sessionId:'a',cancelled:false},{id:'cancelled',itemId:'removed',title:'Undone source',url:'https://example.org/undo',notes:'',at:'2026-10-13T18:00:00Z',sessionId:'a',cancelled:true}];
  expect(showNotes(doc,'a')).toContain('Retained source');expect(showNotes(doc,'a')).not.toContain('Undone source');expect(showNotes(doc,'b')).not.toContain('Takeaway');
 });
 it('exports only shelf segment metadata and preserves item assignments', () => {
  const doc=demoNewsroom();doc.items[2].segmentId='s';doc.segments=[{id:'s',name:'Headlines'},{id:'private',name:'Private segment'}];
  const exported=shelfExport(doc,'next-show');expect(exported.segments).toEqual([{id:'s',name:'Headlines'}]);expect(exported.items[0].segmentId).toBe('s');expect(JSON.stringify(exported)).not.toContain('Private segment');
 });
 it('expires stale moderator availability without claiming current attention', () => {
  const now=Date.parse('2026-10-14T12:00:00Z');const rows=[{actorId:'recent',name:'Recent',status:'green' as const,note:'',setAt:new Date(now-3600000).toISOString()},{actorId:'stale',name:'Stale',status:'green' as const,note:'',setAt:new Date(now-13*3600000).toISOString()},{actorId:'old',name:'Old',status:'green' as const,note:'',setAt:new Date(now-97*3600000).toISOString()}];
  expect(visibleNewsroomRoster(rows,now).map(row=>[row.actorId,row.status])).toEqual([['recent','green'],['stale','red']]);
 });
});

describe('Newsroom newest PR capabilities',()=>{
 it('identifies active review claims and expires them without renewal',()=>{
  const item=demoNewsroom().items[0];item.reviewClaim={actorId:'mod',token:'lease',until:'2026-10-14T03:00:00Z'};
  expect(activeNewsroomClaim(item,Date.parse('2026-10-14T02:59:00Z'))?.actorId).toBe('mod');expect(activeNewsroomClaim(item,Date.parse('2026-10-14T03:00:00Z'))).toBeUndefined();
 });
 it('adds the show recording offset to chapter links',()=>{
  const show={id:'show',title:'Evening',startedAt:'2026-10-14T00:00:00Z',vodUrl:'https://www.youtube.com/watch?v=dQw4w9WgXcQ',vodOffsetSeconds:30,rundown:[],segments:[]};
  expect(newsroomRecordingLink(show,90)).toContain('t=120s');show.vodUrl='https://www.twitch.tv/videos/123';expect(newsroomRecordingLink(show,3600)).toContain('t=1h0m30s');show.vodUrl='';expect(newsroomRecordingLink(show,90)).toBe('');
 });
});

it('reconciles only fields changed in a stale card draft',()=>{
 const base={title:'Derived title',notes:'',warning:''};const draft={...base,warning:'My warning'};const latest={title:'New metadata title',notes:'Concurrent editor takeaway',warning:'Other warning'};
 expect(reconcileNewsroomCardDraft(base,draft,latest)).toEqual({title:'New metadata title',notes:'Concurrent editor takeaway',warning:'My warning'});
});

it('exports the saved bundle snapshot after later bundle edits and lead deletion',()=>{
 const doc=demoNewsroom();doc.items=[];doc.history=[{id:'note',itemId:'gone',title:'Lead',url:'https://example.org/lead',notes:'Takeaway',sessionId:'a',at:'2026-10-14T00:00:00Z',cancelled:false,bundle:{id:'bundle',revision:1,title:'Played story',prepNotes:'Frozen prep',sources:[{title:'Budget',url:'https://example.org/budget',role:'primary_document'}],leadSource:{title:'Lead',url:'https://example.org/lead',kind:'article'}}}];
 doc.bundles=[{...doc.history[0].bundle!,leadId:'',createdAt:'',updatedAt:'',title:'New title',prepNotes:'Later edit'}];
 const text=showNotes(doc,'a');expect(text).toContain('Frozen prep');expect(text).toContain('Primary document: Budget');expect(text).not.toContain('Later edit');
});
it('labels unknown and limited AI input without claiming full video coverage',()=>{
 expect(newsroomEvidenceLabel()).toBe('Input not recorded');expect(newsroomEvidenceLabel({version:1,checkedAt:'',source:'description',inputCharacters:300,truncated:true,outcome:'succeeded',retrieval:'supplied_text'})).toBe('Description only · 300 characters · truncated');
});

it('renders a newly attached bundle when provider coverage is null',()=>{
 const doc=demoNewsroom();const item={...doc.items[0],bundleId:'bundle',enrichment:{coverage:null} as unknown as NonNullable<typeof doc.items[0]['enrichment']>};doc.bundles=[{id:'bundle',revision:0,title:'Story',prepNotes:'',sources:[],leadSource:{title:item.title,url:item.url,kind:item.kind},leadId:item.id,createdAt:'',updatedAt:''}];
 expect(renderToStaticMarkup(<NewsroomBundleCard item={item} doc={doc} command={async()=>true} busy={false} curate/>)).toContain('Save story bundle');
});
