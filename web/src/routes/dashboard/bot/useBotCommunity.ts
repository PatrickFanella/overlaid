import { useEffect, useRef, useState } from 'react';
import {
  deleteBotCounter, deleteBotQuote, fetchBotCounters, fetchBotQuotes, saveBotCounter, saveBotQuote,
  type BotCounter, type BotQuote,
} from '../../../lib/api';
import { defaultCounterDraft, defaultQuoteDraft } from './types';

type Collection<T> = { items: T[]; loadError?: string };
type Pending = { action: 'save' | 'delete'; id?: string };

function message(error: unknown) {
  return error instanceof Error ? error.message : 'Community operation unavailable';
}

// Each collection owns its requests so quote availability cannot block counters.
function useCommunityCollection<T extends { id: string }, D>(
  apiBaseUrl: string, operatorSecret: string, initialDraft: D,
  load: (base: string, secret: string) => Promise<Collection<T>>,
  save: (draft: D, base: string, secret: string) => Promise<T>,
  remove: (id: string, base: string, secret: string) => Promise<void>,
  label: string, onChange: () => Promise<void>,
) {
  const [collection, setCollection] = useState<Collection<T> | null>(null);
  const [draft, setDraft] = useState(initialDraft);
  const [loading, setLoading] = useState(false);
  const [pending, setPending] = useState<Pending | null>(null);
  const [error, setError] = useState('');
  const [result, setResult] = useState('');
  const generation = useRef(0);
  const busy = useRef(false);

  useEffect(() => {
    const current = ++generation.current;
    busy.current = false;
    setCollection(null); setDraft(initialDraft); setLoading(true);
    setPending(null); setError(''); setResult('');
    void load(apiBaseUrl, operatorSecret).then(
      (loaded) => { if (current === generation.current) { setCollection(loaded); setLoading(false); } },
      (failure: unknown) => {
        if (current === generation.current) { setCollection({ items: [], loadError: message(failure) }); setLoading(false); }
      },
    );
    return () => { generation.current += 1; };
  }, [apiBaseUrl, operatorSecret, initialDraft, load]);

  async function perform(operation: Pending, task: () => Promise<T | undefined>) {
    if (loading || busy.current) return;
    busy.current = true;
    const current = generation.current;
    setPending(operation); setError(''); setResult('');
    try {
      const saved = await task();
      if (current !== generation.current) return;
      setCollection((previous) => ({
        ...previous,
        items: saved
          ? [...(previous?.items ?? []).filter((item) => item.id !== saved.id), saved]
          : (previous?.items ?? []).filter((item) => item.id !== operation.id),
      }));
      if (saved) setDraft(initialDraft);
      setResult(`${label} ${saved ? 'saved' : 'deleted'}.`);
      try { await onChange(); }
      catch (failure) {
        if (current === generation.current) setError(`${label} operation succeeded. Status/audit refresh failed: ${message(failure)}`);
      }
    } catch (failure) {
      if (current === generation.current) setError(message(failure));
    } finally {
      if (current === generation.current) { busy.current = false; setPending(null); }
    }
  }

  return {
    collection, draft, loading, pending, error, result,
    onDraftChange: setDraft,
    onSave: () => perform({ action: 'save' }, () => save(draft, apiBaseUrl, operatorSecret)),
    onDelete: (item: T) => perform({ action: 'delete', id: item.id }, async () => { await remove(item.id, apiBaseUrl, operatorSecret); return undefined; }),
  };
}

async function loadCounters(base: string, secret: string): Promise<Collection<BotCounter>> {
  const response = await fetchBotCounters(base, secret);
  return { items: response.counters, loadError: response.loadError };
}
async function loadQuotes(base: string, secret: string): Promise<Collection<BotQuote>> {
  const response = await fetchBotQuotes(base, secret);
  return { items: response.quotes, loadError: response.loadError };
}

export function useBotCommunity(apiBaseUrl: string, operatorSecret: string, onChange: () => Promise<void>) {
  const counters = useCommunityCollection(apiBaseUrl, operatorSecret, defaultCounterDraft, loadCounters, saveBotCounter, deleteBotCounter, 'Counter', onChange);
  const quotes = useCommunityCollection(apiBaseUrl, operatorSecret, defaultQuoteDraft, loadQuotes, saveBotQuote, deleteBotQuote, 'Quote', onChange);
  return { counters, quotes };
}

export type BotCommunityState = ReturnType<typeof useBotCommunity>;
