import { activeNewsroomClaim, type NewsroomItem } from '../../lib/newsroom';
import { Notice } from './ui';
type Command=(action:string,fields?:Record<string,unknown>)=>Promise<boolean>;
export function NewsroomReviewControls({item,actor,owner,command,busy}:{item:NewsroomItem;actor:string;owner:boolean;command:Command;busy:boolean}) {
 const claim=activeNewsroomClaim(item); const mine=claim?.actorId===actor;
 if(item.status!=='pending') return null;
 return <div className="grid gap-2"><p className="desk-field__hint">{claim?`Reviewing: ${mine?'you':claim.actorId} · expires ${new Date(claim.until).toLocaleTimeString()}`:'Available for review'}</p><div className="flex flex-wrap gap-2">{!claim?<button className="desk-btn desk-btn--small" disabled={busy} onClick={()=>{void command('review-start',{id:item.id})}}>Start review</button>:mine?<><button className="desk-btn desk-btn--small" disabled={busy} onClick={()=>{void command('review-extend',{id:item.id,reviewToken:claim.token})}}>Extend review</button><button className="desk-btn desk-btn--small" disabled={busy} onClick={()=>{void command('review-release',{id:item.id,reviewToken:claim.token})}}>Release review</button></>:owner?<button className="desk-btn desk-btn--small desk-btn--ghost" disabled={busy} onClick={()=>{void command('review-release',{id:item.id,reviewToken:claim.token,force:true})}}>Release other reviewer</button>:<Notice>Another reviewer has claimed this card. Their claim must expire or be released before your edits can be saved.</Notice>}</div></div>;
}
