import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { activeNewsroomClaim, demoNewsroom, downloadShowNotes, emptyNewsroom, newsroomRequest, orderedNewsroomItems, showNotes, type NewsroomDocument, type NewsroomItem, type NewsroomProviders, type NewsroomSettings } from '../../lib/newsroom';
import { announceNewsroom, announcementStorageKey, clearAnnouncementIntent, hasAnnouncementIntent, type AnnouncementKind } from '../../lib/newsroomAnnouncement';
import { NewsroomQueuePanel } from './NewsroomQueuePanel';
import { NewsroomEvidence, NewsroomBundleCard, NewsroomBundles } from './NewsroomBundles';
import { NewsroomActivity, NewsroomUsage, NewsroomPreparation } from './NewsroomPreparation';
import { NewsroomBroadcasts } from './NewsroomBroadcasts';
import { LinkRow } from './NewsroomRows';
import { NewsroomReviewControls } from './NewsroomReviewControls';
import { useNewsroomCardDraft } from './useNewsroomCardDraft';
import { NewsroomQuickLinks } from './NewsroomQuickLinks';
import { NewsroomRoster, NewsroomShelfImport, NewsroomShelfItemTools, exportShelf } from './NewsroomLibrary';
import { NewsroomAddToken, NewsroomShelfSharing } from './NewsroomCapabilities';
import { NewsroomAudience, NewsroomImports, NewsroomSegments } from './NewsroomTools';
import { DashboardShell } from './DashboardShell';
import { useDeskStatus } from './deskStatus';
import { useAccount } from './accountSession';
import { Check, Empty, Field, Notice, PageHead, Panel, Tag } from './ui';

type Command = (action: string, fields?: Record<string, unknown>) => Promise<boolean>;
type Tab = 'activity' | 'bundles' | 'shows' | 'deck' | 'shelves' | 'notes' | 'audience' | 'settings';

export function DashboardNewsroomPage() { return <DashboardShell activePath="/dashboard/newsroom"><Newsroom /></DashboardShell>; }

function Newsroom() {
  const { apiBaseUrl, operatorSecret, session } = useDeskStatus();
  const { account } = useAccount();
  const role = account.tenants.find((t) => t.id === account.activeTenantId)?.role;
  const moderator = role === 'moderator';
  const owner = role === 'owner' || account.deploymentMode === 'selfhost' && !role;
  const actor = account.accountId || 'operator';
  const [demo] = useState(() => typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('demo') === '1');
  const [doc, setDoc] = useState<NewsroomDocument>(() => demo ? demoNewsroom() : emptyNewsroom);
  const docRef = useRef(doc);
  const commandChain = useRef<Promise<unknown>>(Promise.resolve());
  const acceptDocument = useCallback((result: NewsroomDocument) => {
    if (result.revision >= docRef.current.revision) { docRef.current = result; setDoc(result); }
  }, []);
  const [loaded, setLoaded] = useState(demo);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [tab, setTab] = useState<Tab>('deck');
  const [selected, setSelected] = useState('');
  const [selectedItem,setSelectedItem] = useState<{item:NewsroomItem;revision:number}|null>(null);
  const [providers, setProviders] = useState<NewsroomProviders | null>(null);
  const announcementScope = {baseUrl:apiBaseUrl,tenantId:account.activeTenantId || 'selfhost-file',actorId:account.accountId || '00000000-0000-4000-8000-000000000002'};
  const announcementScopeRef = useRef('');
  announcementScopeRef.current = announcementStorageKey(announcementScope,'current');
  const [pendingAnnouncements,setPendingAnnouncements] = useState<AnnouncementKind[]>([]);
  const [checkedAnnouncements,setCheckedAnnouncements] = useState(false);
  const refreshAnnouncementIntents = useCallback(() => {
    try { setPendingAnnouncements((['current','raffle'] as const).filter(kind=>hasAnnouncementIntent({baseUrl:apiBaseUrl,tenantId:account.activeTenantId || 'selfhost-file',actorId:account.accountId || '00000000-0000-4000-8000-000000000002'},kind,window.sessionStorage))); }
    catch { setError('Saved announcement requests are unavailable. Check Twitch before starting another announcement.'); }
  },[apiBaseUrl,account.activeTenantId,account.accountId]);
  useEffect(()=>{setBusy(false);setMessage('');setError('');refreshAnnouncementIntents();setCheckedAnnouncements(false);},[refreshAnnouncementIntents]);
  async function announce(kind:AnnouncementKind) {
    if(demo) return;
    const scopeKey=announcementScopeRef.current;
    setBusy(true);setError('');setMessage('');
    try {
      const result=await announceNewsroom(announcementScope,kind,operatorSecret,window.sessionStorage);
      if(announcementScopeRef.current===scopeKey) setMessage(result.message);
    } catch(err) { if(announcementScopeRef.current===scopeKey) setError(String(err)); }
    finally { if(announcementScopeRef.current===scopeKey) {setBusy(false);refreshAnnouncementIntents();setCheckedAnnouncements(false);} }
  }

  const refresh = useCallback(async () => {
    if (demo) return;
    const result = await newsroomRequest<NewsroomDocument>('', 'GET', apiBaseUrl, operatorSecret);
    acceptDocument(result); setLoaded(true);
  }, [apiBaseUrl, operatorSecret, demo, acceptDocument]);
  useEffect(() => {
    let disposed = false;
    async function load() { try { await refresh(); } catch (err) { if (!disposed) setError(String(err)); } }
    void load(); const interval = window.setInterval(() => { if (!busy) void load(); }, 5000);
    return () => { disposed = true; window.clearInterval(interval); };
  }, [refresh, busy]);
  useEffect(() => {
    if (demo || moderator) return;
    newsroomRequest<NewsroomProviders>('/providers', 'GET', apiBaseUrl, operatorSecret).then(setProviders).catch(() => setProviders(null));
  }, [apiBaseUrl, operatorSecret, demo, moderator]);

  const command: Command = (action, fields = {}) => {
    const readItem = doc.items.find(item => item.id === fields.id);
    const editorial = ['approve','reject','edit','display-title','segment','shelve','play','undo-play','undo-reject','focus','delete'].includes(action);
    const captured = {...(editorial && readItem ? {expectedEditorialRevision:readItem.editorialRevision ?? 0} : {}),...fields};
    const run = async () => {
      if (demo) { setMessage('Demo data is for preview. Open Newsroom without ?demo=1 to save changes.'); return false; }
      const quiet = action === 'edit';
      if (!quiet) setBusy(true); setError(''); setMessage('');
      try {
        const result = await newsroomRequest<NewsroomDocument>('', 'POST', apiBaseUrl, operatorSecret, { action, revision: docRef.current.revision, ...captured });
        acceptDocument(result); if (!quiet) setMessage('Saved.'); return true;
      } catch (err) { setError(String(err)); await refresh().catch(() => {}); return false; }
      finally { if (!quiet) setBusy(false); }
    };
    const request = commandChain.current.then(run, run); commandChain.current = request; return request;
  };
  async function providerAction(path: string, body: Record<string, unknown> = {}) {
    if (demo) return;
    setBusy(true); setError(''); setMessage('');
    try { await newsroomRequest(path, 'POST', apiBaseUrl, operatorSecret, body); setMessage(path.endsWith('/enrich') ? 'Enrichment queued. The card updates when it finishes.' : path.endsWith('/discord') ? 'Show notes delivered to Discord.' : path === '/briefing' ? 'Queue briefing updated.' : path === '/playlist' ? 'Playlist imported.' : 'Related coverage loaded.'); await refresh(); }
    catch (err) { setError(String(err)); } finally { setBusy(false); }
  }
  async function tokenRequest(kind: string, shelfId: string, revoke: boolean) {
 if(demo) return ''; setBusy(true);setError('');try {const result=await newsroomRequest<{token:string}>('/token','POST',apiBaseUrl,operatorSecret,{kind,shelfId,revoke});return result.token;} catch(err){setError(String(err));return '';}finally{setBusy(false)}
 }
 const queue = orderedNewsroomItems(doc);
  const focused = selected ? selectedItem?.item.id === selected && doc.revision < selectedItem.revision ? selectedItem.item : doc.items.find(i=>i.id===selected) : doc.items.find(i=>i.id===doc.currentId) ?? queue[0];
  return <>
    <PageHead title="Newsroom" lede="Triage links, prepare the running order, and keep notes as you react." actions={<><Tag>{doc.items.filter((i) => i.status === 'pending').length} pending</Tag><Tag tone="ok">{queue.length} ready</Tag><button className="desk-btn desk-btn--small" disabled={busy || demo} onClick={() => { void refresh().then(() => setError('')).catch((err) => setError(String(err))); }}>Refresh</button></>} />
    {demo ? <Notice>Demo workspace · sample links are separate from your saved queue.</Notice> : null}
    {error ? <Notice tone="bad" role="alert">{error}</Notice> : null}
    {message ? <Notice role="status">{message}</Notice> : null}
    {pendingAnnouncements.length > 0 && !moderator ? <Panel title="Unresolved chat announcement" description="Keep this request until you have checked its receipt or Twitch chat. A missing receipt does not authorize another send.">
      {pendingAnnouncements.map(kind=><div className="flex flex-wrap items-center gap-3" key={kind}><span>{kind==='current'?'Current source':'Raffle'} announcement</span><button className="desk-btn desk-btn--small" disabled={busy} onClick={()=>{void announce(kind);}}>Check receipt</button><button className="desk-btn desk-btn--small desk-btn--ghost" disabled={busy || !checkedAnnouncements} onClick={()=>{try {clearAnnouncementIntent(announcementScope,kind,window.sessionStorage);refreshAnnouncementIntents();setCheckedAnnouncements(false);} catch {setError('The saved announcement request could not be cleared.');}}}>Clear unresolved request</button></div>)}
      <Check label="I checked Twitch chat before clearing this request" checked={checkedAnnouncements} onChange={setCheckedAnnouncements} />
    </Panel> : null}

    <div className="desk-newsroom-tabs" role="group" aria-label="Newsroom views">
      {(['deck', 'shows', 'bundles', 'activity', 'shelves', 'notes', 'audience', 'settings'] as Tab[]).filter((t) => !moderator || t !== 'settings').map((t) => <button key={t} className={`desk-btn ${tab === t ? 'desk-btn--primary' : 'desk-btn--ghost'}`} aria-pressed={tab === t} onClick={() => setTab(t)}>{({ activity:'Moderator activity', bundles:'Story bundles', shows:'Shows and recordings', deck: 'Reaction deck', shelves: 'Shelves', notes: 'Show notes', audience: 'Questions and raffle', settings: 'Newsroom settings' })[t]}</button>)}
    </div>
    {!loaded ? <p role="status">Loading the newsroom…</p> : null}
    {tab === 'deck' ? <>
      <details className="desk-disclosure"><summary>Moderator availability</summary><NewsroomRoster doc={doc} command={command} busy={busy} /></details>
      <SubmitLink command={command} disabled={busy || !loaded} />
      {!moderator ? <><NewsroomImports command={command} busy={busy} playlist={(url) => providerAction('/playlist', { url, revision: doc.revision })} /><NewsroomSegments doc={doc} command={command} busy={busy} /></> : null}
{(doc.quickLinks ?? []).length?<div className="flex flex-wrap gap-2">{doc.quickLinks.map(link=><button className="desk-btn desk-btn--small" key={link.id} disabled={busy} onClick={()=>{void command('submit',{url:link.url})}}>{link.name} +</button>)}</div>:null}
      {!moderator?<details className="desk-disclosure"><summary>Queue briefing</summary><Panel title="Queue briefing" description="An optional AI overview of the approved running order. Review it against the source cards." actions={<button className="desk-btn desk-btn--small" disabled={busy || !providers?.anthropic || !queue.length} onClick={()=>{void providerAction('/briefing')}}>Generate queue briefing</button>}>{doc.briefing?<p>{doc.briefing.summary}</p>:<Empty>Approve and enrich sources before requesting a briefing.</Empty>}</Panel></details>:null}
      <div className="desk-newsroom-layout">
        <div className="grid gap-4">
          <NewsroomQueuePanel doc={doc} apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} command={command} busy={busy} demo={demo} actor={actor} owner={owner} onSelect={(item,revision) => {setSelected(item.id);setSelectedItem({item,revision});void refresh().catch(err=>setError(String(err)));}} />
          <Panel title="Running order" kicker="Approved for the show">
            {queue.length ? queue.map((i) => <LinkRow key={i.id} item={i} selected={focused?.id === i.id} onSelect={() => setSelected(i.id)}>{doc.segments.find(segment=>segment.id===i.segmentId)?<Tag>{doc.segments.find(segment=>segment.id===i.segmentId)?.name} · {doc.segments.find(segment=>segment.id===i.segmentId)?.targetMinutes??'Untimed'}{doc.segments.find(segment=>segment.id===i.segmentId)?.targetMinutes?' planned min':''}</Tag>:null}{!moderator ? <><button className="desk-btn desk-btn--small" disabled={busy} onClick={() => { setSelected(i.id); void command('focus', { id: i.id }); }}>{doc.currentId === i.id ? 'On desk' : 'Put on desk'}</button>{[-1, 1].map((direction) => <button key={direction} className="desk-btn desk-btn--small desk-btn--ghost" aria-label={`${direction < 0 ? 'Move up' : 'Move down'}: ${i.title}`} disabled={busy || queue.indexOf(i) + direction < 0 || queue.indexOf(i) + direction >= queue.length} onClick={() => { const order = [...doc.order]; const index = order.indexOf(i.id); [order[index], order[index + direction]] = [order[index + direction], order[index]]; void command('reorder', { order }); }}>{direction < 0 ? '↑' : '↓'}</button>)}</> : null}</LinkRow>) : <Empty>Approve links to build the running order.</Empty>}
          </Panel>
          <details className="desk-disclosure"><summary>Rejected links ({doc.items.filter((i) => i.status === 'rejected').length})</summary>{doc.items.filter((i) => i.status === 'rejected').map((i) => <LinkRow key={i.id} item={i} onSelect={() => setSelected(i.id)}><button className="desk-btn desk-btn--small" disabled={busy} onClick={() => { void command('approve', { id: i.id }); }}>Restore</button>{!moderator ? <DeleteLink item={i} disabled={busy} command={command} /> : null}</LinkRow>)}</details>
        </div>
        <Panel title="Reaction card" kicker={focused && doc.currentId === focused.id ? 'On desk' : 'Preview'}>
          {focused ? <ReactionCard key={focused.id} item={focused} doc={doc} command={command} busy={busy} moderator={moderator} actor={actor} owner={owner} providers={providers} providerAction={providerAction} announce={()=>announce('current')} /> : <Empty>Select a submitted link to inspect it before approval.</Empty>}
        </Panel>
      </div>
    </> : null}
    {tab === 'bundles' ? <NewsroomBundles doc={doc} command={command} busy={busy} curate={!moderator} apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} demo={demo}/> : null}
    {tab === 'activity' ? <NewsroomActivity apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} demo={demo}/> : null}
    {tab === 'shows' ? <NewsroomBroadcasts doc={doc} command={command} busy={busy} owner={owner} apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} demo={demo} /> : null}
    {tab === 'audience' ? <NewsroomAudience doc={doc} command={command} busy={busy} moderator={moderator} announce={() => announce('raffle')} /> : null}
    {tab === 'shelves' ? <Shelves doc={doc} command={command} busy={busy} moderator={moderator} tokenRequest={tokenRequest} /> : null}
    {tab === 'notes' ? <Notes doc={doc} sessionId={session?.current?.id ?? ''} command={command} busy={busy} moderator={moderator} discord={providers?.discord ?? false} deliver={(sessionId) => providerAction('/notes/discord', { sessionId, confirm: true })} /> : null}
    {tab === 'settings' && !moderator ? <>{owner?<NewsroomUsage command={command} busy={busy} doc={doc} apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} demo={demo}/>:null}<NewsroomQuickLinks doc={doc} command={command} busy={busy}/><NewsroomSettingsPanel tokenRequest={tokenRequest}  settings={doc.settings} providers={providers} command={command} busy={busy} demo={demo} saveKeys={async (keys) => { setBusy(true); setError(''); try { const p = await newsroomRequest<NewsroomProviders>('/providers', 'PUT', apiBaseUrl, operatorSecret, keys); setProviders(p); setMessage('Provider credentials saved.'); } catch (err) { setError(String(err)); } finally { setBusy(false); } }} /></> : null}
  </>;
}

function SubmitLink({ command, disabled }: { command: Command; disabled: boolean }) {
  const [url, setURL] = useState(''); const [title, setTitle] = useState(''); const [submittedBy, setSubmittedBy] = useState('');
  async function submit(e: FormEvent) { e.preventDefault(); if (await command('submit', { url, title, submittedBy })) { setURL(''); setTitle(''); } }
  return <form className="desk-newsroom-submit" onSubmit={(e) => { void submit(e); }}><Field label="Submit a link"><input className="desk-input" type="url" required placeholder="https://…" value={url} onChange={(e) => setURL(e.target.value)} maxLength={4096} /></Field><Field label="Title (optional)"><input className="desk-input" value={title} onChange={(e) => setTitle(e.target.value)} maxLength={300} /></Field><Field label="Submitted by"><input className="desk-input" value={submittedBy} onChange={(e) => setSubmittedBy(e.target.value)} maxLength={100} placeholder="Your name or chat handle" /></Field><button className="desk-btn" type="submit" disabled={disabled}>Add to triage</button></form>;
}

function ReactionCard({ item, doc, command, busy, moderator, actor, owner, providers, providerAction, announce }: { item: NewsroomItem; doc: NewsroomDocument; command: Command; busy: boolean; moderator: boolean; actor:string;owner:boolean; providers: NewsroomProviders | null; providerAction: (path: string) => Promise<void>; announce:()=>Promise<void> }) {
  const review = activeNewsroomClaim(item);
  const claimedElsewhere = !!review && review.actorId !== actor;
  const draft = useNewsroomCardDraft(item, command, item.status === 'played' || claimedElsewhere);
  const { title, notes, warning } = draft;
  const [displayTitle, setDisplayTitle] = useState(item.displayTitle ?? ''); const [shelf, setShelf] = useState(''); const [preview, setPreview]=useState(false);
const enriched = item.enrichment;
 const source=new URL(item.url);const videoID=source.hostname==='www.youtube.com'?source.searchParams.get('v'):null;
 const embed=videoID && /^[A-Za-z0-9_-]+$/.test(videoID)?`https://www.youtube-nocookie.com/embed/${videoID}`:'';
  return <div className="grid gap-4">
    <div className="flex flex-wrap gap-2"><Tag>{item.kind}</Tag><Tag tone={item.status === 'approved' ? 'ok' : 'neutral'}>{item.status}</Tag><Tag tone={enriched?.dmcaRisk === 'high' ? 'bad' : 'warn'}>Copyright risk: {enriched?.dmcaRisk || 'unassessed'}</Tag>{enriched?.credibility ? <Tag>{enriched.credibility}</Tag> : null}</div>
    <NewsroomReviewControls item={item} actor={actor} owner={owner} command={command} busy={busy} />
    <h3 className="desk-newsroom-headline">{item.title}</h3>
    <a className="desk-btn" href={item.url} target="_blank" rel="noreferrer">Open source ↗</a>
    <p className="desk-newsroom-url">{item.url}</p>
    {embed ? <><button className="desk-btn desk-btn--ghost" onClick={()=>setPreview(!preview)}>{preview?'Hide media preview':'Load media preview'}</button>{preview?<iframe className="desk-newsroom-video" src={embed} title={`Preview: ${item.title}`} allow="fullscreen; picture-in-picture" referrerPolicy="no-referrer" allowFullScreen />:null}</>:null}
    {enriched?.body?<details className="desk-disclosure"><summary>Extracted source text / transcript</summary><p className="desk-newsroom-notes">{enriched.body}</p></details>:null}
    {warning || enriched?.contentWarning ? <Notice tone="warn">Content warning: {warning || 'Potentially disturbing imagery. Inspect the source before showing it.'}</Notice> : null}
    {enriched?.summary ? <div><p className="desk-panel__kicker">AI reaction notes · review before use</p><p>{enriched.summary}</p></div> : <Empty>{enriched?.description || 'No summary yet. Enrich this link to fetch its source details.'}</Empty>}
    <NewsroomEvidence enrichment={enriched}/>
    <NewsroomBundleCard item={item} doc={doc} command={command} busy={busy} curate={!moderator}/>
    {enriched?.publisher || enriched?.duration ? <p className="desk-newsroom-row__meta">{enriched.publisher} {enriched.duration ? `· ${enriched.duration}` : ''}</p> : null}
    {enriched?.topics?.length ? <div className="flex flex-wrap gap-2">{enriched.topics.map((topic) => <Tag key={topic}>{topic}</Tag>)}</div> : null}
    {item.processing ? <Tag tone={item.processing.state === 'failed' ? 'bad' : 'neutral'}>Enrichment: {item.processing.state}</Tag> : null}
    {enriched?.error ? <Notice tone="warn">{enriched.error}</Notice> : null}
    {enriched ? <p className="desk-field__hint">AI: {enriched.aiStatus || 'not run'}. Copyright signals are estimates, not permission to rebroadcast.</p> : null}
      <Field label="Card title"><input className="desk-input" disabled={item.status === 'played' || claimedElsewhere} value={title} onChange={(e) => draft.change('title', e.target.value)} onBlur={() => { void draft.flush(); }} maxLength={300} /></Field><Field label="Takeaway / show notes"><textarea className="desk-input" disabled={item.status === 'played' || claimedElsewhere} rows={4} value={notes} onChange={(e) => draft.change('notes', e.target.value)} onBlur={() => { void draft.flush(); }} maxLength={8000} /></Field><Field label="Content warning"><input className="desk-input" disabled={item.status === 'played' || claimedElsewhere} value={warning} onChange={(e) => draft.change('warning', e.target.value)} onBlur={() => { void draft.flush(); }} maxLength={500} /></Field>
      <Field label="Show segment"><select className="desk-input" value={item.segmentId ?? ''} onChange={(e) => { void command('segment', { id: item.id, segmentId: e.target.value }); }} disabled={busy || moderator}><option value="">Ungrouped</option>{doc.segments.map((s) => <option value={s.id} key={s.id}>{s.name}</option>)}</select></Field>
      <button className="desk-btn" disabled={busy || item.status === 'played' || claimedElsewhere} onClick={() => { void draft.flush(); }}>Save card notes</button><p role="status" className="desk-field__hint">{draft.status || 'Card changes save automatically and when you switch items.'}</p>{draft.status.startsWith('Card not saved') ? <Notice tone="warn"><p>Latest saved takeaway: {item.notes || '(empty)'}</p><div className="flex flex-wrap gap-2"><button className="desk-btn" disabled={busy} onClick={() => { void draft.retry(); }}>Save my draft over the current card</button><button className="desk-btn desk-btn--ghost" disabled={busy} onClick={draft.discard}>Discard draft and load saved card</button></div></Notice> : null}
      <Field label="Overlay / chat title" hint="Leave blank to use the source title. Show notes keep the source title."><input className="desk-input" value={displayTitle} onChange={(e) => setDisplayTitle(e.target.value)} maxLength={200} /></Field><button className="desk-btn" disabled={busy || moderator} onClick={() => { void command('display-title', {id:item.id,displayTitle}); }}>Save broadcast title</button>
    {!moderator ? <><div className="flex flex-wrap gap-2"><button className="desk-btn desk-btn--small" disabled={busy || !['approved', 'played'].includes(item.status) || item.processing?.state === 'running'} onClick={() => { void providerAction(`/items/${item.id}/enrich`); }}>Enrich / retry</button><button className="desk-btn desk-btn--small desk-btn--ghost" disabled={busy || !providers?.brave} onClick={() => { void providerAction(`/items/${item.id}/coverage`); }}>Find related coverage</button></div>
      {enriched?.coverage?.length ? <ul className="grid gap-2">{enriched.coverage.map((c) => <li key={c.url}><a href={c.url} target="_blank" rel="noreferrer">{c.title} ↗</a></li>)}</ul> : null}
      {item.status === 'approved' ? <div className="flex flex-wrap gap-2"><button className="desk-btn" disabled={busy || doc.currentId === item.id} onClick={() => { void command('focus', { id: item.id }); }}>Put on desk</button><button className="desk-btn desk-btn--ghost" disabled={busy || doc.currentId !== item.id} onClick={() => { void announce(); }}>Post source to Twitch chat</button><button className="desk-btn desk-btn--primary" disabled={busy || doc.currentId !== item.id} onClick={() => { void draft.flush().then((saved) => { if (saved) void command('play', { id: item.id, expectedEditorialRevision:draft.revision() }); }); }}>Mark played</button>{doc.currentId === item.id ? <button className="desk-btn desk-btn--ghost" disabled={busy} onClick={() => { void command('stop'); }}>Clear desk</button> : null}</div> : null}
      {item.status !== 'played' ? <div className="flex flex-wrap gap-2"><select className="desk-input" aria-label="Destination shelf" value={shelf} onChange={(e) => setShelf(e.target.value)}><option value="">Choose a shelf…</option>{doc.shelves.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}</select><button className="desk-btn" disabled={busy || !shelf} onClick={() => { void command('shelve', { id: item.id, shelfId: shelf }); }}>Save for later</button></div> : null}
    </> : null}
  </div>;
}

function DeleteLink({ item, disabled, command }: { item: NewsroomItem; disabled: boolean; command: Command }) {
  const [confirm, setConfirm] = useState(false);
  return <button className="desk-btn desk-btn--small desk-btn--ghost" disabled={disabled} onClick={() => { if (!confirm) { setConfirm(true); return; } void command('delete', { id: item.id }); }}>{confirm ? 'Confirm permanent removal' : 'Remove'}</button>;
}
function Shelves({ doc, command, busy, moderator, tokenRequest }: { doc: NewsroomDocument; command: Command; busy: boolean; moderator: boolean; tokenRequest:(kind:string,shelfId:string,revoke:boolean)=>Promise<string> }) {
  const [name, setName] = useState('');
  return <div className="grid gap-4">{!moderator ? <form className="flex flex-wrap items-end gap-3" onSubmit={async (e) => { e.preventDefault(); if (await command('create-shelf', { name })) setName(''); }}><Field label="New shelf"><input className="desk-input" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} placeholder="Next show, research, evergreen…" /></Field><button className="desk-btn" disabled={busy}>Create shelf</button></form> : null}{!moderator?<NewsroomShelfImport command={command} busy={busy}/>:null}{doc.shelves.length ? doc.shelves.map((s) => <Panel key={s.id} title={s.name} actions={!moderator ? <><button className="desk-btn desk-btn--small" disabled={!doc.items.some(i=>i.shelfId===s.id)} onClick={()=>exportShelf(doc,s.id)}>Export shelf</button><button className="desk-btn desk-btn--small desk-btn--ghost" disabled={busy || doc.items.some((i) => i.shelfId === s.id)} onClick={() => { void command('delete-shelf', { id: s.id }); }}>Remove empty shelf</button></> : null}>{doc.items.filter((i) => i.shelfId === s.id).map((i) => <article className="desk-newsroom-row" key={i.id}><strong>{i.title}</strong><a href={i.url} target="_blank" rel="noreferrer" className="desk-newsroom-url">{i.url}</a><NewsroomShelfItemTools item={i} doc={doc} command={command} busy={busy} /></article>)}<NewsroomPreparation shelf={s} doc={doc} command={command} busy={busy} curate={!moderator}/>{!moderator ? <NewsroomShelfSharing shelfId={s.id} tokenRequest={tokenRequest} busy={busy} /> : null}{!doc.items.some((i) => i.shelfId === s.id) ? <Empty>Select a link in the deck and save it to this shelf.</Empty> : null}</Panel>) : <Empty>Create a shelf to prepare material for another show.</Empty>}</div>;
}
function Notes({ doc, sessionId, command, busy, moderator, discord, deliver }: { doc: NewsroomDocument; sessionId: string; command: Command; busy: boolean; moderator: boolean; discord: boolean; deliver: (sessionId: string) => Promise<void> }) {
  const [scope, setScope] = useState(''); const [confirm, setConfirm] = useState(false);
  const sessions = [...new Set((doc.history ?? doc.items.filter((i) => i.status === 'played')).map((i) => i.sessionId).filter(Boolean))];
  const text = showNotes(doc, scope); const items = (doc.history ?? doc.items.filter((i) => i.status === 'played')).filter((i) => !('cancelled' in i && i.cancelled) && (!scope || i.sessionId === scope));
  return <Panel title="Show notes" description="Played links retain their takeaways and the stream session in which they were used." actions={<><select className="desk-input" aria-label="Show notes session" value={scope} onChange={(e) => { setScope(e.target.value); setConfirm(false); }}><option value="">All played links</option>{sessions.map((s) => <option key={s} value={s}>{s === sessionId ? 'Current session' : `Session ${s.slice(0, 8)}`}</option>)}</select><button className="desk-btn" disabled={!items.length} onClick={() => downloadShowNotes(text)}>Download Markdown</button>{!moderator ? <button className="desk-btn" disabled={busy || !discord || !items.length} onClick={() => { if (!confirm) { setConfirm(true); return; } setConfirm(false); void deliver(scope); }}>{confirm ? 'Confirm post to Discord' : 'Post to Discord'}</button> : null}</>}><pre className="desk-newsroom-notes">{text}</pre>{items.map((i) => <div className="flex items-center justify-between gap-4" key={i.id}><span>{i.title}</span>{!moderator && 'itemId' in i && doc.items.some(item => item.id === i.itemId && item.status === 'played') ? <button className="desk-btn desk-btn--small" disabled={busy} onClick={() => { void command('undo-play', {id:i.itemId}); }}>Undo played</button> : null}</div>)}</Panel>;
}

function NewsroomSettingsPanel({ settings, providers, command, busy, demo, saveKeys,tokenRequest }: { tokenRequest:(kind:string,shelfId:string,revoke:boolean)=>Promise<string>; settings: NewsroomSettings; providers: NewsroomProviders | null; command: Command; busy: boolean; demo: boolean; saveKeys: (keys: Record<string, string>) => Promise<void> }) {
  const [value, setValue] = useState(settings); const [keys, setKeys] = useState<Record<string, string>>({}); const [removeKey,setRemoveKey]=useState('');
  return <div className="grid gap-4"><Panel title="Submission handling" description="Uses your connected channel and the existing EventSub worker. Capture continues while the desk is closed."><form className="grid gap-4" onSubmit={(e) => { e.preventDefault(); void command('settings', { settings: value }); }}><Notice>Manage submission, current-source, question and raffle commands in <a href="/dashboard/chat#bot-commands">Chat → Bot commands</a>. That is the only place to rename, enable, set roles or set cooldowns.</Notice><Check label="Allow duplicate links" checked={value.allowDuplicates} onChange={(allowDuplicates) => setValue({ ...value, allowDuplicates })} /><Check label="Automatically request AI analysis after approval" checked={value.autoEnrich} onChange={(autoEnrich) => setValue({ ...value, autoEnrich })} /><Field label="Preferred coverage sources" hint="Comma-separated publisher domains, used to order related coverage."><input className="desk-input" value={(value.preferredSources ?? []).join(', ')} onChange={(e)=>setValue({...value,preferredSources:e.target.value.split(',').map(s=>s.trim().toLowerCase()).filter(Boolean)})}/></Field><Field label="Ignored Twitch logins" hint="Separate names with commas."><input className="desk-input" value={value.ignoredUsers.join(', ')} onChange={(e) => setValue({ ...value, ignoredUsers: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) })} /></Field><p className="desk-field__hint">Automatic analysis uses your provider account and may incur charges. Up to three links per submission. Command cooldowns and permissions are configured in Chat.</p><button className="desk-btn" disabled={busy} type="submit">Save submission settings</button></form></Panel>
    <Panel title="Media and enrichment providers" description="Credentials are write-only and encrypted with your workspace’s vault keyring. Empty fields keep saved values."><form className="grid gap-4" onSubmit={async (e) => { e.preventDefault(); await saveKeys(Object.fromEntries(Object.entries(keys).filter(([, value]) => value.trim()))); setKeys({}); }}>{(['anthropic', 'youtube', 'brave', 'discord'] as const).map((name) => <div className="grid gap-2" key={name}><Field label={`${({ anthropic: 'Anthropic API key', youtube: 'YouTube Data API key', brave: 'Brave Search API key', discord: 'Discord webhook URL' })[name]} · ${providers?.[name] ? 'configured' : 'not configured'}`}><input className="desk-input" type="password" autoComplete="new-password" value={keys[name] ?? ''} maxLength={4096} onChange={(e) => setKeys({ ...keys, [name]: e.target.value })} /></Field><button className="desk-btn desk-btn--small desk-btn--ghost" type="button" disabled={busy || !providers?.canSaveKeys} onClick={async()=>{if(removeKey!==name){setRemoveKey(name);return}await saveKeys({[name]:''});setRemoveKey('')}}>{removeKey===name?'Confirm remove saved override':'Remove saved override'}</button></div>)}{!providers?.canSaveKeys ? <Notice tone="warn">The operator must configure VAULT_KEYS and persistent storage before credentials can be saved. Self-host installations can also use provider environment variables.</Notice> : null}<button className="desk-btn" disabled={busy || demo || !providers?.canSaveKeys} type="submit">Save provider credentials</button></form><p className="desk-field__hint">Without provider keys, links and show notes still work. Article details are fetched from the source; YouTube uses oEmbed or the Data API. AI results and copyright risk signals need human review.</p></Panel>
    <Panel title="Browser quick-add"><NewsroomAddToken tokenRequest={tokenRequest} busy={busy || demo} /></Panel>
  </div>;
}
