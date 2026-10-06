import { useEffect, useRef, useState } from 'react';
import {
  deleteBotTimer, fetchBotTimers, runBotTimer, saveBotTimer, setBotTimerEnabled,
  type BotTimer, type BotTimersResponse,
} from '../../../lib/api';
import { defaultTimerDraft, type TimerDraft } from './types';

type TimerOperation = { action: 'save' | 'toggle' | 'delete' | 'run'; id?: string };
type TimerChange = { message: string; timer?: BotTimer; deletedId?: string };

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : 'Timer operation unavailable';
}

export function useBotTimers(apiBaseUrl: string, operatorSecret: string, onChange: () => Promise<void>) {
  const [timers, setTimers] = useState<BotTimersResponse | null>(null);
  const [draft, setDraft] = useState<TimerDraft>(defaultTimerDraft);
  const [pending, setPending] = useState<TimerOperation | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [result, setResult] = useState('');
  const generation = useRef(0);
  const busy = useRef(false);

  useEffect(() => {
    const current = ++generation.current;
    busy.current = false;
    setPending(null);
    setLoading(true);
    setTimers(null);
    setDraft(defaultTimerDraft);
    setError('');
    setResult('');
    void fetchBotTimers(apiBaseUrl, operatorSecret).then(
      (loaded) => {
        if (generation.current === current) { setTimers(loaded); setLoading(false); }
      },
      (failure: unknown) => {
        if (generation.current === current) {
          setTimers({ timers: [], loadError: errorMessage(failure) });
          setLoading(false);
        }
      },
    );
    return () => { generation.current += 1; };
  }, [apiBaseUrl, operatorSecret]);

  async function perform(operation: TimerOperation, task: () => Promise<TimerChange>) {
    if (busy.current) return;
    busy.current = true;
    const current = generation.current;
    setPending(operation);
    setError('');
    setResult('');
    try {
      const change = await task();
      if (generation.current !== current) return;
      if (change.timer) {
        const saved = change.timer;
        setTimers((previous) => ({
          timers: [...(previous?.timers ?? []).filter((timer) => timer.id !== saved.id), saved].sort((left, right) => left.name.localeCompare(right.name)),
        }));
        if (operation.action === 'toggle') {
          setDraft((previous) => previous.id === saved.id ? { ...previous, enabled: saved.enabled } : previous);
        }
      }
      if (change.deletedId) {
        setTimers((previous) => ({ timers: (previous?.timers ?? []).filter((timer) => timer.id !== change.deletedId) }));
        setDraft((previous) => previous.id === change.deletedId ? defaultTimerDraft : previous);
      }
      if (operation.action === 'save') setDraft(defaultTimerDraft);
      setResult(change.message);
      try { await onChange(); }
      catch (failure) {
        if (generation.current === current) setError(`Timer operation succeeded. Status/audit refresh failed: ${errorMessage(failure)}`);
      }
    } catch (failure) {
      if (generation.current === current) setError(errorMessage(failure));
    } finally {
      if (generation.current === current) { busy.current = false; setPending(null); }
    }
  }

  function onEdit(timer: BotTimer) {
    if (busy.current) return;
    setDraft({ id: timer.id, name: timer.name, enabled: timer.enabled, intervalSeconds: timer.intervalSeconds, response: timer.response, action: timer.action === 'alert' ? 'alert' : 'ticker' });
    setError('');
    setResult('');
  }

  return {
    timers, draft, loading, error, result, pending,
    onDraftChange: setDraft,
    onEdit,
    onCancel: () => { if (!busy.current) { setDraft(defaultTimerDraft); setError(''); } },
    onSave: () => perform({ action: 'save', id: draft.id }, async () => {
      const timer = await saveBotTimer(draft, apiBaseUrl, operatorSecret);
      return { timer, message: `${draft.id ? 'Updated' : 'Created'} ${timer.name}.` };
    }),
    onToggle: (timer: BotTimer) => perform({ action: 'toggle', id: timer.id }, async () => {
      const updated = await setBotTimerEnabled(timer.id, !timer.enabled, apiBaseUrl, operatorSecret);
      return { timer: updated, message: `${updated.enabled ? 'Enabled' : 'Paused'} ${updated.name}.` };
    }),
    onDelete: (timer: BotTimer) => perform({ action: 'delete', id: timer.id }, async () => {
      await deleteBotTimer(timer.id, apiBaseUrl, operatorSecret);
      return { deletedId: timer.id, message: `Deleted ${timer.name}.` };
    }),
    onRun: (timer: BotTimer) => perform({ action: 'run', id: timer.id }, async () => {
      const sent = await runBotTimer(timer.id, apiBaseUrl, operatorSecret);
      return { message: `Sent ${timer.name}. Event ${sent.eventId}. Schedule unchanged.` };
    }),
  };
}

export type BotTimersState = ReturnType<typeof useBotTimers>;
