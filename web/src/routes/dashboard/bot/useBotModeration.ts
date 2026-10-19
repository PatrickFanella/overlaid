import { useEffect, useRef, useState } from 'react';
import {
  deleteBotModerationRule, dryRunBotModeration,
  fetchBotModerationRules, rollbackBotNukeRadiation, saveBotModerationRule,
  type BotModerationActionResponse, type BotModerationDryRunResponse, type BotModerationRule,
  type BotModerationRulesResponse, type BotNukeDryRunResponse, type BotNukeExecuteResponse,
} from '../../../lib/api';
import { canSubmitNuke, clearNukeIntent, loadNukeIntent, previewNuke, submitNuke, type NukePreview, type NukeOutcome } from '../../../lib/nukeIntent';
import { useAccount } from '../accountSession';
import { clearModerationIntent, hasModerationIntent, moderationIntentKey, runModerationIntent } from '../../../lib/moderationIntent';
import { defaultModerationAction, defaultModerationDraft, defaultModerationTest, defaultNukeDraft, type NukeDraft } from './types';

type Operation = 'save' | 'delete' | 'dry-run' | 'action' | 'nuke-plan' | 'nuke-execute' | 'nuke-rollback';
function message(error: unknown) { return error instanceof Error ? error.message : 'Moderation operation unavailable'; }

export function useBotModeration(apiBaseUrl: string, operatorSecret: string, onChange: () => Promise<void>) {
  const { account } = useAccount();
  const scope = {baseUrl:apiBaseUrl, tenantId:account.activeTenantId || 'selfhost-file', actorId:account.accountId || '00000000-0000-4000-8000-000000000002'};
  const scopeKey = moderationIntentKey(scope);
  const [unresolvedAction, setUnresolvedAction] = useState(false);
  const [checkedAction, setCheckedAction] = useState(false);
  const [rules, setRules] = useState<BotModerationRulesResponse | null>(null);
  const [draft, setDraft] = useState(defaultModerationDraft);
  const [test, setTest] = useState(defaultModerationTest);
  const [dryRun, setDryRun] = useState<BotModerationDryRunResponse | null>(null);
  const [actionDraft, setActionDraft] = useState(defaultModerationAction);
  const [actionResult, setActionResult] = useState<BotModerationActionResponse | null>(null);
  const [nukeDraft, setNukeDraft] = useState(defaultNukeDraft);
  const [nukeResult, setNukeResult] = useState<BotNukeDryRunResponse | null>(null);
  const [nukeExecuteResult, setNukeExecuteResult] = useState<BotNukeExecuteResponse | null>(null);
  const [savedNuke, setSavedNuke] = useState<NukePreview | null>(null);
  const [unresolvedNuke, setUnresolvedNuke] = useState(false);
  const [checkedNuke, setCheckedNuke] = useState(false);
  const [nukeOutcome, setNukeOutcome] = useState<NukeOutcome | null>(null);
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
    setActionDraft(defaultModerationAction); setActionResult(null); setCheckedAction(false);
    try { setUnresolvedAction(hasModerationIntent(scope, window.sessionStorage)); }
    catch { setUnresolvedAction(true); setError('Saved moderation requests are unavailable. Check Twitch before starting another action.'); }
    setNukeDraft(defaultNukeDraft); setNukeResult(null); setNukeExecuteResult(null); setNukeRollbackResult('');
    setSavedNuke(null); setNukeOutcome(null); setCheckedNuke(false);
    try {
      const saved = loadNukeIntent(scope, window.sessionStorage);
      setSavedNuke(saved); setUnresolvedNuke(Boolean(saved?.requestKey));
      if (saved) { setNukeDraft({ ...defaultNukeDraft, ...saved.draft, confirm: false }); setNukeResult(saved.display); }
    } catch { setUnresolvedNuke(true); setNukeError('Saved nuke requests are unavailable. Check Twitch before clearing them.'); }
    void fetchBotModerationRules(apiBaseUrl, operatorSecret).then(
      (loaded) => { if (current === generation.current) { setRules(loaded); setLoading(false); } },
      (failure: unknown) => { if (current === generation.current) { setRules({ rules: [], loadError: message(failure) }); setLoading(false); } },
    );
    return () => { generation.current += 1; };
  // scopeKey includes API, workspace and account identity.
  }, [apiBaseUrl, operatorSecret, scopeKey]);

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
    if (busy.current || unresolvedNuke) return;
    const changed = Object.keys(defaultNukeDraft).some((key) => key !== 'confirm' && next[key as keyof NukeDraft] !== nukeDraft[key as keyof NukeDraft]);
    if (changed) {
      try { clearNukeIntent(scope, window.sessionStorage); } catch { setNukeError('The saved preview could not be cleared.'); return; }
      setSavedNuke(null);
      setNukeDraft({ ...next, confirm: false }); setNukeResult(null); setNukeError('');
    } else {
      setNukeDraft({ ...next, confirm: Boolean(next.confirm && canSubmitNuke(savedNuke)) });
    }
  }

  return {
    rules, draft, test, dryRun, actionDraft, actionResult, nukeDraft, nukeResult, nukeExecuteResult, nukeRollbackResult,
    loading, pending, error, result, nukeError, unresolvedAction, checkedAction,
    savedNuke, unresolvedNuke, checkedNuke, nukeOutcome, canReviewNuke: canSubmitNuke(savedNuke),
    onClearNuke: () => {
      if (busy.current || !checkedNuke) return;
      try { clearNukeIntent(scope, window.sessionStorage); setSavedNuke(null); setUnresolvedNuke(false); setCheckedNuke(false); setNukeResult(null); setNukeOutcome(null); setNukeDraft((p) => ({ ...p, confirm: false })); }
      catch { setNukeError("The saved nuke request could not be cleared."); }
    },
    onClearAction: () => {
      if (busy.current || !checkedAction) return;
      try { clearModerationIntent(scope, window.sessionStorage); setUnresolvedAction(false); setCheckedAction(false); setActionResult(null); }
      catch { setError('The saved moderation request could not be cleared.'); }
    },
    onDraftChange: setDraft,
    onEdit: (rule: BotModerationRule) => {
      if (busy.current) return;
      setDraft({
        id: rule.id, name: rule.name, enabled: rule.enabled, enforce: Boolean(rule.enforce),
        pattern: rule.pattern, matchTarget: rule.matchTarget as typeof draft.matchTarget,
        caseSensitive: rule.caseSensitive, regex: rule.regex, action: rule.action as typeof draft.action,
        reason: rule.reason || '', exemptRole: rule.exemptRole || 'moderator', timeoutSeconds: rule.timeoutSeconds || 60,
      });
    },
    onTestChange: (next: typeof test) => { if (!busy.current) { setTest(next); setDryRun(null); } },
    onActionChange: (next: typeof actionDraft) => { if (!busy.current && !unresolvedAction) { setActionDraft(next); setActionResult(null); } },
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
      const actionGeneration = generation.current;
      return perform('action', async () => {
        try { return await runModerationIntent(scope, actionDraft, operatorSecret, window.sessionStorage); }
        finally {
          // perform's generation fence also protects scope changes during await.
          if (generation.current === actionGeneration) {
            setCheckedAction(true);
            try { setUnresolvedAction(hasModerationIntent(scope, window.sessionStorage)); }
            catch { setUnresolvedAction(true); }
          }
        }
      }, setActionResult, true);
    },
    onNukeDryRun: () => {
      if (busy.current || unresolvedNuke) return;
      setNukeResult(null); setSavedNuke(null); setNukeDraft((previous) => ({ ...previous, confirm: false }));
      setNukeExecuteResult(null); setNukeOutcome(null); setNukeRollbackResult('');
      return perform('nuke-plan', () => previewNuke(scope, nukeDraft, operatorSecret, window.sessionStorage), (preview) => {
        setSavedNuke(preview); setNukeResult(preview.display); setNukeDraft({ ...defaultNukeDraft, ...preview.draft, confirm: false });
      });
    },
    onNukeExecute: () => {
      if (busy.current || !unresolvedNuke && (!canSubmitNuke(savedNuke) || !nukeDraft.confirm)) return;
      setNukeDraft((previous) => ({ ...previous, confirm: false })); setNukeRollbackResult('');
      const current = generation.current;
      return perform('nuke-execute', async () => {
        try { return await submitNuke(scope, operatorSecret, window.sessionStorage); }
        finally {
          if (current === generation.current) {
            setCheckedNuke(true);
            try { const saved = loadNukeIntent(scope, window.sessionStorage); setSavedNuke(saved); setUnresolvedNuke(Boolean(saved?.requestKey)); }
            catch { setUnresolvedNuke(true); }
          }
        }
      }, (outcome) => {
        setNukeOutcome(outcome);
        if (outcome.direct) { setNukeExecuteResult(outcome.direct); if (outcome.direct.radiationRule) updateRule(outcome.direct.radiationRule); }
        if (outcome.radiationRuleId) {
          // Rule existence/enabled state is independent from an old receipt.
          void fetchBotModerationRules(apiBaseUrl, operatorSecret).then((loaded) => { if (current === generation.current) setRules(loaded); }, () => { if (current === generation.current) setNukeError('Request status confirmed. Refresh rules to check the radiation window.'); });
        }
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
