import { requestJSON } from './apiRequest';

export type NewsroomInputEvidence = {version:number;checkedAt:string;source:string;inputCharacters:number;truncated:boolean;outcome:string;model?:string;retrieval:string};
export type NewsroomBundleSource = {title:string;url:string;role:'coverage'|'primary_document'|'clip'};
export type NewsroomBundleSnapshot = {id:string;title:string;prepNotes:string;sources:NewsroomBundleSource[];leadSource:{title:string;url:string;kind:string};revision:number};
export type NewsroomBundle = NewsroomBundleSnapshot & {leadId:string;createdAt:string;updatedAt:string};
export type NewsroomEnrichment = {
  extractionEvidence?:NewsroomInputEvidence; aiInputEvidence?:NewsroomInputEvidence;
  title: string; publisher: string; description: string; body: string; thumbnail: string; duration: string;
  summary: string; credibility: string; topics: string[]; dmcaRisk: string; contentWarning: boolean;
  coverage: { title: string; url: string }[]; error: string; aiStatus: string; updatedAt: string;
};
export type NewsroomItem = {
  bundleId?:string; editorialRevision?: number; reviewClaim?: {actorId:string;token:string;until:string};
  id: string; url: string; title: string; kind: string; status: 'pending' | 'approved' | 'played' | 'rejected' | 'shelved';
  submittedBy: string; notes: string; warning: string; shelfId: string; sessionId: string; createdAt: string; playedAt?: string;
  approvedBy?: string; displayTitle?: string; processing?: {state:string;ai:boolean};
  segmentId?: string; enrichment?: NewsroomEnrichment;
};
export type NewsroomSettings = {
  allowDuplicates: boolean; autoEnrich: boolean; ignoredUsers: string[]; preferredSources: string[];
};
export type NewsroomDocument = {
  bundles?:NewsroomBundle[];
  history?: {bundle?:NewsroomBundleSnapshot;broadcastId?:string;offsetSeconds?:number;id:string;itemId:string;title:string;url:string;notes:string;sessionId:string;at:string;cancelled:boolean}[];
  revision: number; items: NewsroomItem[]; shelves: { id: string; name: string }[]; order: string[];
  segments: { id: string; name: string }[]; questions: { id: string; text: string; askedBy: string; status: string }[]; currentQuestion: string; raffle: { open: boolean; command: string; entries: { login: string }[]; winner: string };
  quickLinks: {id:string;name:string;url:string}[]; briefing?:{summary:string;updatedAt:string};
  roster: {actorId:string;name:string;status:'green'|'yellow'|'red';note:string;setAt:string}[];
  broadcasts?: NewsroomBroadcast[];
  currentId: string; settings: NewsroomSettings;
};
export type NewsroomProviders = { anthropic: boolean; youtube: boolean; brave: boolean; discord: boolean; canSaveKeys: boolean; queued: number };
export const emptyNewsroom: NewsroomDocument = {
  revision: 0, items: [], shelves: [], order: [], currentId: '', segments: [], questions: [], roster: [], quickLinks: [], currentQuestion: '', raffle: { open: false, command: '!join', entries: [], winner: '' },
  settings: { allowDuplicates: false, autoEnrich: false, ignoredUsers: [], preferredSources: [] },
};
export function newsroomRequest<T>(path: string, method: 'GET' | 'POST' | 'PUT', baseUrl: string, secret: string, body?: Record<string, unknown>) {
  return requestJSON<T>(`/api/newsroom${path}`, { method, explicitBaseUrl: baseUrl, operatorSecret: secret, body, errorLabel: 'Newsroom' });
}
export function orderedNewsroomItems(doc: NewsroomDocument) {
  const byId = new Map(doc.items.map((item) => [item.id, item]));
  const ordered = doc.order.flatMap((id) => { const item = byId.get(id); return item?.status === 'approved' ? [item] : []; });
  return [...doc.segments.flatMap((segment) => ordered.filter((item) => item.segmentId === segment.id)), ...ordered.filter((item) => !doc.segments.some((s) => s.id === item.segmentId))];
}
export function showNotes(doc: NewsroomDocument, sessionId = '') {
  const played = (doc.history ?? doc.items.filter(item => item.status === 'played').map(item => ({...item,at:item.playedAt ?? '',cancelled:false}))).filter(item => !item.cancelled && (!sessionId || item.sessionId === sessionId)).sort((a,b) => a.at.localeCompare(b.at));
  return `# Show notes\n\n${played.length ? played.map((item) => `- ${item.title.replace(/[\r\n]/g, ' ')} — ${item.url}${item.notes ? `\n  ${item.notes.replace(/\n/g, '\n  ')}` : ''}${'bundle' in item && item.bundle ? `\n\n${newsroomBundleMarkdown(item.bundle)}` : ''}`).join('\n') : 'No played links in this selection.'}\n`;
}
export function downloadShowNotes(text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: 'text/markdown;charset=utf-8' }));
  const link = document.createElement('a'); link.href = url; link.download = 'show-notes.md'; link.click(); URL.revokeObjectURL(url);
}
export function demoNewsroom(): NewsroomDocument {
  const item = (id: string, title: string, status: NewsroomItem['status']): NewsroomItem => ({ id, title, status, url: `https://example.org/${id}`, kind: 'article', submittedBy: 'demo_viewer', notes: '', warning: '', shelfId: '', sessionId: '', createdAt: '2026-10-13T19:00:00Z' });
  return { ...emptyNewsroom, revision: 1, items: [item('community', '[Demo] Community radio opens a new studio', 'approved'), item('transit', '[Demo] City publishes its transit budget', 'pending'), { ...item('library', '[Demo] Library extends evening hours', 'shelved'), shelfId: 'next-show' }], shelves: [{ id: 'next-show', name: 'Next show' }], order: ['community'], currentId: 'community' };
}

export function shelfExport(doc: NewsroomDocument, id: string) {
 const items = doc.items.filter(item => item.shelfId === id).map(item => ({title:item.title,url:item.url,segmentId:item.segmentId ?? ''}));
 return {name:doc.shelves.find(shelf => shelf.id === id)?.name ?? '',items,segments:doc.segments.filter(segment => items.some(item => item.segmentId === segment.id))};
}
export function visibleNewsroomRoster(roster: NewsroomDocument['roster'], now = Date.now()) {
 return roster.filter(row => now - Date.parse(row.setAt) <= 96 * 3600000).map(row => now - Date.parse(row.setAt) > 12 * 3600000 ? {...row,status:'red' as const,note: row.note ? `${row.note} · status expired` : 'Status expired'} : row);
}

export type NewsroomBroadcast = {id:string;title:string;startedAt:string;endedAt?:string;vodUrl:string;vodOffsetSeconds:number;sessionId?:string;rundown:{id:string;title:string;url:string;kind:string;segmentId:string;bundle?:NewsroomBundleSnapshot}[];segments:{id:string;name:string}[]};
export type NewsroomQueuePage = {revision:number;items:NewsroomItem[];total:number;totals:Record<string,number>;nextCursor:string};
export function newsroomRecordingLink(show:NewsroomBroadcast,offset:number) {
 if (!show.vodUrl) return '';
 const url=new URL(show.vodUrl);const seconds=Math.max(0,Math.floor(offset+show.vodOffsetSeconds));
 url.searchParams.set('t',url.hostname==='www.twitch.tv'?`${Math.floor(seconds/3600)}h${Math.floor(seconds%3600/60)}m${seconds%60}s`:`${seconds}s`);return url.toString();
}
export function activeNewsroomClaim(item:NewsroomItem,now=Date.now()) {return item.reviewClaim && Date.parse(item.reviewClaim.until)>now ? item.reviewClaim : undefined;}

export function reconcileNewsroomCardDraft(base:Pick<NewsroomItem,'title'|'notes'|'warning'>,draft:Pick<NewsroomItem,'title'|'notes'|'warning'>,latest:Pick<NewsroomItem,'title'|'notes'|'warning'>) {
 return {title:draft.title!==base.title?draft.title:latest.title,notes:draft.notes!==base.notes?draft.notes:latest.notes,warning:draft.warning!==base.warning?draft.warning:latest.warning};
}

export function newsroomBundleMarkdown(bundle:NewsroomBundleSnapshot) {
 return `### Story bundle: ${bundle.title.replace(/[\r\n]/g,' ')}\n\n${bundle.prepNotes ? bundle.prepNotes+'\n\n' : ''}${bundle.sources.map(source=>`- ${{coverage:'Coverage',primary_document:'Primary document',clip:'Clip'}[source.role]}: ${source.title.replace(/[\r\n]/g,' ')} — ${source.url}`).join('\n')}\n`;
}
export function newsroomEvidenceLabel(evidence?:NewsroomInputEvidence) {
 if (!evidence) return 'Input not recorded';
 return `${({page_text:'Source page text',page_metadata:'Page metadata',transcript:'Video transcript',description:'Description only',post_text:'Post text',unavailable:'Input unavailable'} as Record<string,string>)[evidence.source] ?? evidence.source} · ${evidence.inputCharacters.toLocaleString()} characters${evidence.truncated ? ' · truncated' : ''}`;
}
