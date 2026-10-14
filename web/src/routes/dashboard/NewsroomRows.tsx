import {useState,type ReactNode} from 'react';
import type {NewsroomItem,NewsroomDocument} from '../../lib/newsroom';
type Command=(action:string,fields?:Record<string,unknown>)=>Promise<boolean>;
export function LinkRow({ item, selected = false, onSelect, children }: { item: NewsroomItem; selected?: boolean; onSelect: () => void; children?: ReactNode }) {
  return <article className={`desk-newsroom-row ${selected ? 'desk-newsroom-row--selected' : ''}`}><button className="desk-newsroom-row__select" onClick={onSelect}><span className="desk-newsroom-row__meta">{item.kind} · {item.submittedBy || 'Operator'} {item.enrichment?.contentWarning || item.warning ? '· Content warning' : ''}</span><strong>{item.title}</strong><span className="desk-newsroom-url">{item.url}</span>{item.approvedBy ? <span className="desk-field__hint">Approved by {item.approvedBy}</span> : null}</button><a className="desk-btn desk-btn--small" href={item.url} target="_blank" rel="noreferrer">Open source ↗</a><div className="flex flex-wrap gap-2">{children}</div></article>;
}

export function ApproveToSegment({item,doc,command,busy}:{item:NewsroomItem;doc:NewsroomDocument;command:Command;busy:boolean}) {
 const [segmentId,setSegmentId]=useState(item.segmentId ?? '');
 return <><select className="desk-input" aria-label={`Approve ${item.title} into segment`} disabled={busy} value={segmentId} onChange={event=>setSegmentId(event.target.value)}><option value="">Ungrouped</option>{doc.segments.map(segment=><option key={segment.id} value={segment.id}>{segment.name}</option>)}</select><button className="desk-btn desk-btn--small" disabled={busy} onClick={()=>{void command('approve',{id:item.id,segmentId,expectedEditorialRevision:item.editorialRevision ?? 0})}}>Approve</button></>;
}
