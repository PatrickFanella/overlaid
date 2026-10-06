import { useEffect, useRef, useState } from 'react';
import {
  deleteBotAPIVariable, fetchBotAPIVariables, saveBotAPIVariable,
  type BotAPIVariable, type BotAPIVariablesResponse,
} from '../../../lib/api';
import { defaultAPIVariableDraft, type APIVariableDraft } from './types';

type VariableOperation = { action: 'save' | 'delete'; id?: string };
type VariableChange = { message: string; variable?: BotAPIVariable; deletedId?: string };

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : 'API variable operation unavailable';
}

export function useBotAPIVariables(apiBaseUrl: string, operatorSecret: string, onChange: () => Promise<void>) {
  const [variables, setVariables] = useState<BotAPIVariablesResponse | null>(null);
  const [draft, setDraft] = useState<APIVariableDraft>(defaultAPIVariableDraft);
  const [pending, setPending] = useState<VariableOperation | null>(null);
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
    setVariables(null);
    setDraft(defaultAPIVariableDraft);
    setError('');
    setResult('');
    void fetchBotAPIVariables(apiBaseUrl, operatorSecret).then(
      (loaded) => {
        if (generation.current === current) { setVariables(loaded); setLoading(false); }
      },
      (failure: unknown) => {
        if (generation.current === current) {
          setVariables({ variables: [], loadError: errorMessage(failure) });
          setLoading(false);
        }
      },
    );
    return () => { generation.current += 1; };
  }, [apiBaseUrl, operatorSecret]);

  async function perform(operation: VariableOperation, task: () => Promise<VariableChange>) {
    if (loading || busy.current) return;
    busy.current = true;
    const current = generation.current;
    setPending(operation);
    setError('');
    setResult('');
    try {
      const change = await task();
      if (generation.current !== current) return;
      if (change.variable) {
        const saved = change.variable;
        setVariables((previous) => ({
          ...previous,
          variables: [...(previous?.variables ?? []).filter((variable) => variable.id !== saved.id), saved].sort((left, right) => left.name.localeCompare(right.name)),
        }));
      }
      if (change.deletedId) {
        setVariables((previous) => ({ ...previous, variables: (previous?.variables ?? []).filter((variable) => variable.id !== change.deletedId) }));
        setDraft((previous) => previous.id === change.deletedId ? defaultAPIVariableDraft : previous);
      }
      if (operation.action === 'save') setDraft(defaultAPIVariableDraft);
      setResult(change.message);
      try { await onChange(); }
      catch (failure) {
        if (generation.current === current) setError(`API variable operation succeeded. Status/audit refresh failed: ${errorMessage(failure)}`);
      }
    } catch (failure) {
      if (generation.current === current) setError(errorMessage(failure));
    } finally {
      if (generation.current === current) { busy.current = false; setPending(null); }
    }
  }

  function onEdit(variable: BotAPIVariable) {
    if (busy.current) return;
    setDraft({ id: variable.id, name: variable.name, enabled: variable.enabled, url: variable.url,
      responseSelector: variable.responseSelector || '', fallback: variable.fallback || '',
      cacheTtlSeconds: variable.cacheTtlSeconds, timeoutMs: variable.timeoutMs });
    setError('');
    setResult('');
  }

  return {
    variables, draft, loading, error, result, pending,
    onDraftChange: setDraft,
    onEdit,
    onCancel: () => { if (!busy.current) { setDraft(defaultAPIVariableDraft); setError(''); } },
    onSave: () => perform({ action: 'save', id: draft.id }, async () => {
      const variable = await saveBotAPIVariable(draft, apiBaseUrl, operatorSecret);
      return { variable, message: `${draft.id ? 'Updated' : 'Created'} ${variable.name}.` };
    }),
    onDelete: (variable: BotAPIVariable) => perform({ action: 'delete', id: variable.id }, async () => {
      await deleteBotAPIVariable(variable.id, apiBaseUrl, operatorSecret);
      return { deletedId: variable.id, message: `Deleted ${variable.name}.` };
    }),
  };
}

export type BotAPIVariablesState = ReturnType<typeof useBotAPIVariables>;
