import { useEffect, useRef, useState } from 'react';
import {
  deleteBotModerationRule, dryRunBotModeration, dryRunBotNuke, executeBotModerationAction,
  executeBotNuke, fetchBotModerationRules, rollbackBotNukeRadiation, saveBotModerationRule,
  type BotModerationActionResponse, type BotModerationDryRunResponse, type BotModerationRule,
  type BotModerationRulesResponse, type BotNukeDryRunResponse, type BotNukeExecuteResponse,
} from '../../../lib/api';
import { defaultModerationAction, defaultModerationDraft, defaultModerationTest, defaultNukeDraft, type NukeDraft } from './types';

type Operation = 'save' | 'delete' | 'dry-run' | 'action' | 'nuke-plan' | 'nuke-execute' | 'nuke-rollback';
function message(error: unknown) { return error instanceof Error ? error.message : 'Moderation operation unavailable'; }

export function useBotModeration(apiBaseUrl: string, operatorSecret: string, onChange: () => Promise<void>) {
  const [rules, setRules] = useState<BotModerationRulesResponse | null>(null);
  const [draft, setDraft] = useState(defaultModerationDraft);
  const [test, setTest] = useState(defaultModerationTest);
  const [dryRun, setDryRun] = useState<BotModerationDryRunResponse | null>(null);
  const [actionDraft, setActionDraft] = useState(defaultModerationAction);
  const [actionResult, setActionResult] = useState<BotModerationActionResponse | null>(null);
  const [nukeDraft, setNukeDraft] = useState(defaultNukeDraft);
  const [nukeResult, setNukeResult] = useState<BotNukeDryRunResponse | null>(null);
  const [nukeExecuteResult, setNukeExecuteResult] = useState<BotNukeExecuteResponse | null>(null);
  const [nukeRollbackResult, setNukeRollbackResult] = useState('');
  const [loading, setLoading] = useState(false);
  const [pending, setPending] = useState<Operation | null>(null);
  const [error, setError] = useState('');
  const [result, setResult] = useState('');
  const [nukeError, setNukeError] = useState('');
  const generation = useRef(0);
  const busy = useRef(false);

  useEffect(() => {
    const current = ++generation.current;
    busy.current = false;
    setRules(null); setLoading(true); setPending(null); setError(''); setResult(''); setNukeError('');
    setDraft(defaultModerationDraft); setTest(defaultModerationTest); setDryRun(null);
    setActionDraft(defaultModerationAction); setActionResult(null);
    setNukeDraft(defaultNukeDraft); setNukeResult(null); setNukeExecuteResult(null); setNukeRollbackResult('');
    void fetchBotModerationRules(apiBaseUrl, operatorSecret).then(
      (loaded) => { if (current === generation.current) { setRules(loaded); setLoading(false); } },
      (failure: unknown) => { if (current === generation.current) { setRules({ rules: [], loadError: message(failure) }); setLoading(false); } },
    );
    return () => { generation.current += 1; };
  }, [apiBaseUrl, operatorSecret]);

  function updateRule(rule: BotModerationRule) {
    setRules((previous) => ({ ...previous, rules: [...(previous?.rules ?? []).filter((item) => item.id !== rule.id), rule].sort((a, b) => a.name.localeCompare(b.name)) }));
  }

  async function perform<T>(operation: Operation, task: () => Promise<T>, apply: (value: T) => void, refresh = false) {
    if (busy.current) return;
    busy.current = true;
    const current = generation.current;
    const setOperationError = operation.startsWith('nuke-') ? setNukeError : setError;
    setPending(operation); setOperationError('');
    try {
      const value = await task();
      if (current !== generation.current) return;
      apply(value);
      if (refresh) {
        try { await onChange(); }
        catch (failure) { if (current === generation.current) setOperationError(`Operation completed. Status/audit refresh failed: ${message(failure)}`); }
      }
    } catch (failure) {
      if (current === generation.current) {
        setOperationError(message(failure));
        // Failed Twitch attempts may also have audit rows. Keep their refresh
        // separate from the original execution failure.
        if (refresh) { try { await onChange(); } catch { /* Original failure stays visible. */ } }
      }
    } finally {
      if (current === generation.current) { busy.current = false; setPending(null); }
    }
  }

  function onNukeDraftChange(next: NukeDraft) {
    if (busy.current) return;
    const changed = Object.keys(defaultNukeDraft).some((key) => key !== 'confirm' && next[key as keyof NukeDraft] !== nukeDraft[key as keyof NukeDraft]);
    if (changed) {
      setNukeDraft({ ...next, confirm: false }); setNukeResult(null); setNukeError('');
    } else {
      setNukeDraft({ ...next, confirm: Boolean(next.confirm && nukeResult?.matches.length) });
    }
  }

  return {
    rules, draft, test, dryRun, actionDraft, actionResult, nukeDraft, nukeResult, nukeExecuteResult, nukeRollbackResult,
    loading, pending, error, result, nukeError,
    onDraftChange: setDraft,
    onTestChange: (next: typeof test) => { if (!busy.current) { setTest(next); setDryRun(null); } },
    onActionChange: (next: typeof actionDraft) => { if (!busy.current) { setActionDraft(next); setActionResult(null); } },
    onNukeDraftChange,
    onSave: () => {
      if (loading || busy.current) return;
      setResult('');
      return perform('save', () => saveBotModerationRule(draft, apiBaseUrl, operatorSecret), (saved) => { updateRule(saved); setDraft(defaultModerationDraft); setDryRun(null); setResult(`Saved ${saved.name}.`); }, true);
    },
    onDelete: (rule: BotModerationRule) => {
      if (loading || busy.current) return;
      setResult('');
      return perform('delete', () => deleteBotModerationRule(rule.id, apiBaseUrl, operatorSecret), () => {
        setRules((previous) => ({ ...previous, rules: (previous?.rules ?? []).filter((item) => item.id !== rule.id) }));
        setDryRun(null); setResult(`Deleted ${rule.name}.`);
      }, true);
    },
    onDryRun: () => { if (!busy.current) { setDryRun(null); return perform('dry-run', () => dryRunBotModeration(test, apiBaseUrl, operatorSecret), setDryRun); } },
    onAction: () => {
      if (busy.current) return;
      setActionResult(null);
      return perform('action', () => executeBotModerationAction(actionDraft, apiBaseUrl, operatorSecret), setActionResult, true);
    },
    onNukeDryRun: () => {
      if (busy.current) return;
      setNukeResult(null); setNukeDraft((previous) => ({ ...previous, confirm: false }));
      setNukeExecuteResult(null); setNukeRollbackResult('');
      return perform('nuke-plan', () => dryRunBotNuke({ ...nukeDraft, confirm: false }, apiBaseUrl, operatorSecret), setNukeResult);
    },
    onNukeExecute: () => {
      if (busy.current || !nukeResult?.matches.length || !nukeDraft.confirm) return;
      setNukeDraft((previous) => ({ ...previous, confirm: false })); setNukeExecuteResult(null); setNukeRollbackResult('');
      return perform('nuke-execute', () => executeBotNuke(nukeDraft, apiBaseUrl, operatorSecret), (executed) => {
        setNukeExecuteResult(executed); if (executed.radiationRule) updateRule(executed.radiationRule);
      }, true);
    },
    onNukeRollback: (rule: BotModerationRule) => {
      if (busy.current) return;
      setNukeRollbackResult('');
      return perform('nuke-rollback', () => rollbackBotNukeRadiation(rule.id, apiBaseUrl, operatorSecret), (rolledBack) => {
        updateRule(rolledBack.rule); setNukeRollbackResult(`disabled ${rolledBack.rule.name}`);
      }, true);
    },
  };
}

export type BotModerationState = ReturnType<typeof useBotModeration>;
