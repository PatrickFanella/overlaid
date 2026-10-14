import { useCallback, useEffect, useRef, useState } from 'react';
import { reconcileNewsroomCardDraft, type NewsroomItem } from '../../lib/newsroom';

type Values = { title: string; notes: string; warning: string };
type Draft = { value: Values; base: Values; revision:number; failed?:boolean };
type Command = (action: string, fields?: Record<string, unknown>) => Promise<boolean>;
const same = (a: Values, b: Values) => a.title === b.title && a.notes === b.notes && a.warning === b.warning;
const values = (item: NewsroomItem): Values => ({ title: item.title, notes: item.notes, warning: item.warning });
export function useNewsroomCardDraft(item: NewsroomItem, command: Command, disabled: boolean) {
  const key = `overlaid.newsroom.draft.${item.id}`;
  const [draft, setDraft] = useState<Draft>(() => {
    const base = values(item);
    try { const saved = JSON.parse(sessionStorage.getItem(key) || 'null') as Draft | null;
      if (saved && [saved.value, saved.base].every((v) => v && ['title', 'notes', 'warning'].every((name) => typeof v[name as keyof Values] === 'string'))) return {...saved,revision:Number.isSafeInteger(saved.revision)?saved.revision:item.editorialRevision ?? 0,failed:saved.failed || !Number.isSafeInteger(saved.revision)};
    } catch { /* A draft is optional; retained server data remains authoritative. */ }
    return { value: base, base, revision:item.editorialRevision ?? 0 };
  });
  const [status, setStatus] = useState(draft.failed?'Card not saved. Your draft is retained; review the error and retry.':'');
  const current = useRef(draft); const latestCommand = useRef(command); latestCommand.current = command;
  const failed = useRef(draft.failed ?? false);
  const latestItem = useRef(item); latestItem.current = item;
  const pending = useRef<Promise<boolean> | null>(null); const mounted = useRef(true);
  const store = useCallback((next: Draft) => {
    current.current = next;
    try { if (same(next.value, next.base)) sessionStorage.removeItem(key); else sessionStorage.setItem(key, JSON.stringify(next)); } catch { /* Saving to the API still works when browser storage is unavailable. */ }
    if (mounted.current) setDraft(next);
  }, [key]);
  const flush = useCallback(async (): Promise<boolean> => {
    if (failed.current) return false;
    if (disabled || same(current.current.value, current.current.base)) return true;
    if (pending.current) return pending.current;
    const snapshot = current.current;
    if (mounted.current) setStatus('Saving card…');
    const request = latestCommand.current('edit', { id: item.id, expectedEditorialRevision:snapshot.revision, ...(snapshot.value.title!==snapshot.base.title?{title:snapshot.value.title,expectedTitle:snapshot.base.title}:{}), ...(snapshot.value.notes!==snapshot.base.notes?{notes:snapshot.value.notes,expectedNotes:snapshot.base.notes}:{}), ...(snapshot.value.warning!==snapshot.base.warning?{warning:snapshot.value.warning,expectedWarning:snapshot.base.warning}:{}) }).then(ok => {
      failed.current = !ok;
      if (ok) { store({ value: current.current.value, base: snapshot.value, revision:snapshot.revision+1 }); if (mounted.current) setStatus('Card saved.'); }
      else {store({...current.current,failed:true});if (mounted.current) setStatus('Card not saved. Your draft is retained; review the error and retry.');}
      return ok;
    }).finally(() => { pending.current = null; });
    // Every caller waits for both persistence and the local revision update.
    pending.current = request;
    return request;

  }, [disabled, item.id, store]);
  const flushRef = useRef(flush); flushRef.current = flush;
  useEffect(() => {
    mounted.current = true;
    return () => { void flushRef.current(); mounted.current = false; };
  }, []);
  useEffect(() => {
    const base = values(item);
    if (same(current.current.value, current.current.base) && !pending.current) store({ value: base, base, revision:item.editorialRevision ?? 0 });
  }, [item.title, item.notes, item.warning, item, store]);
  useEffect(() => {
    if (same(draft.value, draft.base) || disabled || status.startsWith('Card not saved')) return;
    const timer = window.setTimeout(() => { void flush(); }, 600);
    return () => window.clearTimeout(timer);
  }, [draft, disabled, flush, status]);
  const change = (field: keyof Values, text: string) => { setStatus(failed.current ? 'Card not saved. Your draft is retained; review the error and retry.' : 'Unsaved card changes.'); store({ ...current.current, value: { ...current.current.value, [field]: text } }); };
  const retry = async () => {
    if (pending.current) await pending.current;
    const base=values(latestItem.current);
    const value=reconcileNewsroomCardDraft(current.current.base,current.current.value,base);
    store({value,base,revision:latestItem.current.editorialRevision ?? 0});failed.current=false;setStatus('');return flush();
  };
  const discard = () => { failed.current=false; const base = values(item); store({value:base,base,revision:item.editorialRevision ?? 0}); setStatus('Loaded the saved card.'); };
  return { ...draft.value, change, flush, retry, discard, status, revision:()=>current.current.revision };
}
