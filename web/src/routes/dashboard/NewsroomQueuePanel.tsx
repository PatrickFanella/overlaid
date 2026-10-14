import { useEffect, useRef, useState } from 'react';
import { activeNewsroomClaim, newsroomRequest, type NewsroomDocument, type NewsroomItem, type NewsroomQueuePage } from '../../lib/newsroom';
import { LinkRow, ApproveToSegment } from './NewsroomRows';
import { NewsroomReviewControls } from './NewsroomReviewControls';
import { Empty, Field, Notice, Panel } from './ui';
type Command=(action:string,fields?:Record<string,unknown>)=>Promise<boolean>;
export function NewsroomQueuePanel({doc,apiBaseUrl,operatorSecret,command,busy,demo,actor,owner,onSelect}:{doc:NewsroomDocument;apiBaseUrl:string;operatorSecret:string;command:Command;busy:boolean;demo:boolean;actor:string;owner:boolean;onSelect:(item:NewsroomItem,revision:number)=>void}) {
 const [query,setQuery]=useState('');const [status,setStatus]=useState('pending');const [kind,setKind]=useState('');
 const [page,setPage]=useState<NewsroomQueuePage|null>(null);const [loadedKey,setLoadedKey]=useState('');const [loading,setLoading]=useState(false);const [error,setError]=useState('');const [retry,setRetry]=useState(0);
 const [undo,setUndo]=useState<{id:string;revision:number}|null>(null);
 const docRef=useRef(doc);docRef.current=doc;
 const generation=useRef(0);const currentKey=JSON.stringify([query,status,kind]);const keyRef=useRef(currentKey);keyRef.current=currentKey;
 const actionable=loadedKey===currentKey && !loading && !busy;
 useEffect(()=>{
  const run=++generation.current;setLoading(true);setError('');setPage(null);setLoadedKey('');
  const timer=window.setTimeout(()=>{
    if(demo){setPage({revision:docRef.current.revision,items:docRef.current.items.filter(item=>(!status||item.status===status)&&(!kind||item.kind===kind)&&`${item.title} ${item.url} ${item.submittedBy}`.toLowerCase().includes(query.toLowerCase())),total:docRef.current.items.length,totals:{pending:docRef.current.items.filter(item=>item.status==='pending').length},nextCursor:''});setLoadedKey(currentKey);setLoading(false);return;}
    const params=new URLSearchParams({q:query,status,kind});
    newsroomRequest<NewsroomQueuePage>(`/queue?${params}`,'GET',apiBaseUrl,operatorSecret).then(result=>{if(generation.current===run){setPage(result);setLoadedKey(currentKey);setLoading(false);}}).catch(err=>{if(generation.current===run){setError(String(err));setLoading(false);}});
  },200);
  return ()=>{window.clearTimeout(timer);generation.current++;};
 },[query,status,kind,currentKey,demo,apiBaseUrl,operatorSecret,retry]);
 const visibleItems=(page?.items??[]).flatMap(item=>{const live=doc.items.find(row=>row.id===item.id);if(!live && doc.revision >= (page?.revision ?? 0)) return [];const shown=live&&(live.editorialRevision??0)>=(item.editorialRevision??0)?live:item;return (!status||shown.status===status)&&(!kind||shown.kind===kind)?[shown]:[];});
 const matchingTotal=page?Math.max(0,page.total-(page.items.length-visibleItems.length)):0;
 async function more(){if(!page?.nextCursor||!actionable)return;const run=++generation.current;const captured=currentKey;setLoading(true);setError('');try{const params=new URLSearchParams({q:query,status,kind,cursor:page.nextCursor});const result=await newsroomRequest<NewsroomQueuePage>(`/queue?${params}`,'GET',apiBaseUrl,operatorSecret);if(generation.current===run&&keyRef.current===captured){setPage(previous=>previous?{...result,items:[...previous.items,...result.items.filter(item=>!previous.items.some(old=>old.id===item.id))]}:result);setLoading(false);}}catch(err){if(generation.current===run){setError(String(err));setLoading(false);}}}
 return <Panel title="Moderator queue" kicker="Triage"><div className="flex flex-wrap items-end gap-2"><Field label="Search links" hint="Titles, source URLs, publishers, topics, and submitters."><input className="desk-input" maxLength={300} value={query} onChange={e=>setQuery(e.target.value)} /></Field><Field label="Queue status"><select className="desk-input" value={status} onChange={e=>setStatus(e.target.value)}>{['pending','approved','played','rejected','shelved',''].map(value=><option key={value} value={value}>{value||'All retained links'}</option>)}</select></Field><button className="desk-btn desk-btn--small" disabled={loading} onClick={()=>setRetry(value=>value+1)}>Refresh search</button><Field label="Media type"><select className="desk-input" value={kind} onChange={e=>setKind(e.target.value)}>{['','article','youtube','social','twitch'].map(value=><option key={value} value={value}>{value||'All media'}</option>)}</select></Field></div>
 {error?<Notice tone="bad" role="alert">{error}<button className="desk-btn" onClick={()=>setRetry(value=>value+1)}>Retry search</button></Notice>:null}
 {loading?<p role="status">Searching retained links…</p>:null}
 {undo?<Notice><button className="desk-btn desk-btn--small" disabled={busy} onClick={()=>{void command('undo-reject',{id:undo.id,expectedEditorialRevision:undo.revision}).then(ok=>{if(ok)setUndo(null)})}}>Undo last rejection</button></Notice>:null}
 {page&&loadedKey===currentKey?<><p className="desk-field__hint">{matchingTotal} matching · {visibleItems.length} loaded · {doc.items.filter(item=>item.status==='pending').length} pending across the workspace</p>{visibleItems.map(shown=>{const claim=activeNewsroomClaim(shown);const locked=!!claim&&claim.actorId!==actor;return <LinkRow key={shown.id} item={shown} onSelect={()=>{if(actionable)onSelect(shown,page.revision)}}><NewsroomReviewControls item={shown} actor={actor} owner={owner} command={command} busy={!actionable}/>{shown.status==='pending'?<><ApproveToSegment item={shown} doc={doc} command={command} busy={!actionable||locked}/><button className="desk-btn desk-btn--small desk-btn--ghost" disabled={!actionable||locked} onClick={()=>{void command('reject',{id:shown.id,expectedEditorialRevision:shown.editorialRevision??0}).then(ok=>{if(ok)setUndo({id:shown.id,revision:(shown.editorialRevision??0)+1})})}}>Reject</button></>:null}</LinkRow>})}{!visibleItems.length?<Empty>No retained links match this search.</Empty>:null}{page.nextCursor?<button className="desk-btn" disabled={!actionable} onClick={()=>{void more()}}>Load more links</button>:null}</>:null}
 </Panel>;
}
