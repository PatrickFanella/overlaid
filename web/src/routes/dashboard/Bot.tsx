import { useEffect, useState } from 'react';
import {
  deleteBotCommand,
  deleteBotCounter,
  deleteBotAPIVariable,
  deleteBotModerationRule,
  deleteBotQuote,
  deleteBotTimer,
  dryRunBotModeration,
  dryRunBotNuke,
  executeBotNuke,
  executeBotModerationAction,
  fetchBotAudit,
  fetchBotAPIVariables,
  fetchBotCommands,
  fetchBotCounters,
  fetchBotModerationRules,
  fetchBotQuotes,
  fetchBotStatus,
  fetchBotTimers,
  rollbackBotNukeRadiation,
  saveBotCommand,
  saveBotCounter,
  saveBotAPIVariable,
  saveBotModerationRule,
  saveBotQuote,
  saveBotTimer,
  runBotTimer,
  setBotCommandEnabled,
  setBotTimerEnabled,
  type BotNukeDryRunResponse,
  type BotNukeExecuteResponse,
  type BotAPIVariable,
  type BotCommand,
  type BotCounter,
  type BotModerationRule,
  type BotQuote,
  type BotTimer,
} from '../../lib/api';
import { loadOperatorSecret } from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { BotStatusSummary } from './bot/BotStatusSummary';
import {
  defaultAPIVariableDraft,
  defaultCommandDraft,
  defaultCounterDraft,
  defaultModerationAction,
  defaultModerationDraft,
  defaultModerationTest,
  defaultNukeDraft,
  defaultQuoteDraft,
  defaultTimerDraft,
  type BotPageState,
  type CommandDraft,
  type ModerationDraft,
  type TimerDraft,
} from './bot/types';
import { CustomizablePanelLayout } from './components/CustomizablePanelLayout';
import type { PanelLayoutItem } from '../../lib/dashboardLayout';
import { loadDashboardConfig, saveDashboardConfig } from '../../lib/dashboardConfig';
import { Notice } from './ui';
import { BotCommandsPanel } from './bot/BotCommandsPanel';
import { BotCommunityPanel } from './bot/BotCommunityPanel';
import { BotAPIVariablesPanel } from './bot/BotAPIVariablesPanel';
import { BotModerationPanel } from './bot/BotModerationPanel';
import { BotTimersPanel } from './bot/BotTimersPanel';

export function BotWorkspace() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const operatorSecret = loadOperatorSecret();
  const [config, setConfig] = useState(() => loadDashboardConfig());
  const [state, setState] = useState<BotPageState>({ status: null, audit: null, commands: null, counters: null, quotes: null, apiVariables: null, moderation: null, moderationDryRun: null, timers: null, loading: false, error: '' });
  const [commandDraft, setCommandDraft] = useState<CommandDraft>(defaultCommandDraft);
  const [draft, setDraft] = useState<TimerDraft>(defaultTimerDraft);
  const [savingCommand, setSavingCommand] = useState(false);
  const [savingTimer, setSavingTimer] = useState(false);
  const [runningTimer, setRunningTimer] = useState<string | null>(null);
  const [timerRunResult, setTimerRunResult] = useState('');
  const [counterDraft, setCounterDraft] = useState(defaultCounterDraft);
  const [quoteDraft, setQuoteDraft] = useState(defaultQuoteDraft);
  const [apiVariableDraft, setAPIVariableDraft] = useState(defaultAPIVariableDraft);
  const [moderationDraft, setModerationDraft] = useState<ModerationDraft>(defaultModerationDraft);
  const [moderationTest, setModerationTest] = useState(defaultModerationTest);
  const [moderationAction, setModerationAction] = useState(defaultModerationAction);
  const [moderationActionResult, setModerationActionResult] = useState('');
  const [nukeDraft, setNukeDraft] = useState(defaultNukeDraft);
  const [nukeResult, setNukeResult] = useState<BotNukeDryRunResponse | null>(null);
  const [nukeExecuteResult, setNukeExecuteResult] = useState<BotNukeExecuteResponse | null>(null);
  const [nukeRollbackResult, setNukeRollbackResult] = useState('');

  function updateBotPanelLayout(items: PanelLayoutItem[]) {
    setConfig((current) => {
      const next = { ...current, panelLayouts: { ...current.panelLayouts, bot: items } };
      saveDashboardConfig(next);
      return next;
    });
  }

  useEffect(() => {
    let cancelled = false;
    async function load() {
      setState((current) => ({ ...current, loading: true, error: '' }));
      try {
        const [status, audit, commands, counters, quotes, apiVariables, moderation, timers] = await Promise.all([fetchBotStatus(apiBaseUrl, operatorSecret), fetchBotAudit(25, apiBaseUrl, operatorSecret), fetchBotCommands(apiBaseUrl, operatorSecret), fetchBotCounters(apiBaseUrl, operatorSecret), fetchBotQuotes(apiBaseUrl, operatorSecret), fetchBotAPIVariables(apiBaseUrl, operatorSecret), fetchBotModerationRules(apiBaseUrl, operatorSecret), fetchBotTimers(apiBaseUrl, operatorSecret)]);
        if (!cancelled) {
          setState({ status, audit, commands, counters, quotes, apiVariables, moderation, moderationDryRun: null, timers, loading: false, error: '' });
        }
      } catch (error) {
        if (!cancelled) {
          setState((current) => ({ ...current, loading: false, error: error instanceof Error ? error.message : 'bot status unavailable' }));
        }
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, [apiBaseUrl, operatorSecret]);

  async function refreshTimers() {
    const [status, timers, audit] = await Promise.all([fetchBotStatus(apiBaseUrl, operatorSecret), fetchBotTimers(apiBaseUrl, operatorSecret), fetchBotAudit(25, apiBaseUrl, operatorSecret)]);
    setState((current) => ({ ...current, status, timers, audit }));
  }

  async function refreshCommands() {
    const [status, commands] = await Promise.all([fetchBotStatus(apiBaseUrl, operatorSecret), fetchBotCommands(apiBaseUrl, operatorSecret)]);
    setState((current) => ({ ...current, status, commands }));
  }

  async function refreshCommunity() {
    const [status, counters, quotes, apiVariables] = await Promise.all([fetchBotStatus(apiBaseUrl, operatorSecret), fetchBotCounters(apiBaseUrl, operatorSecret), fetchBotQuotes(apiBaseUrl, operatorSecret), fetchBotAPIVariables(apiBaseUrl, operatorSecret)]);
    setState((current) => ({ ...current, status, counters, quotes, apiVariables }));
  }

  async function refreshModeration() {
    const moderation = await fetchBotModerationRules(apiBaseUrl, operatorSecret);
    setState((current) => ({ ...current, moderation }));
  }

  async function handleSaveModerationRule() {
    try { await saveBotModerationRule(moderationDraft, apiBaseUrl, operatorSecret); setModerationDraft(defaultModerationDraft); await refreshModeration(); }
    catch (error) { setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'moderation rule save failed' })); }
  }

  async function handleDeleteModerationRule(rule: BotModerationRule) {
    try { await deleteBotModerationRule(rule.id, apiBaseUrl, operatorSecret); await refreshModeration(); }
    catch (error) { setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'moderation rule delete failed' })); }
  }

  async function handleModerationDryRun() {
    try { const moderationDryRun = await dryRunBotModeration(moderationTest, apiBaseUrl, operatorSecret); setState((current) => ({ ...current, moderationDryRun, error: '' })); }
    catch (error) { setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'moderation dry-run failed' })); }
  }

  async function handleModerationAction() {
    try {
      const result = await executeBotModerationAction(moderationAction, apiBaseUrl, operatorSecret);
      setModerationActionResult(`${result.action} ${result.result}`);
      setState((current) => ({ ...current, error: '' }));
    } catch (error) {
      setModerationActionResult('');
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'moderation action failed' }));
    }
  }

  async function handleNukeDryRun() {
    try {
      const result = await dryRunBotNuke(nukeDraft, apiBaseUrl, operatorSecret);
      setNukeResult(result);
      setNukeExecuteResult(null);
      setNukeRollbackResult('');
      setNukeDraft((current) => ({ ...current, confirm: false }));
      setState((current) => ({ ...current, error: '' }));
    } catch (error) {
      setNukeResult(null);
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'nuke dry-run failed' }));
    }
  }

  async function handleNukeExecute() {
    try {
      const result = await executeBotNuke(nukeDraft, apiBaseUrl, operatorSecret);
      setNukeExecuteResult(result);
      setNukeRollbackResult('');
      setNukeDraft((current) => ({ ...current, confirm: false }));
      await refreshModeration();
      setState((current) => ({ ...current, error: '' }));
    } catch (error) {
      setNukeExecuteResult(null);
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'nuke execute failed' }));
    }
  }

  async function handleNukeRollback(rule: BotModerationRule) {
    try {
      const result = await rollbackBotNukeRadiation(rule.id, apiBaseUrl, operatorSecret);
      setNukeRollbackResult(`disabled ${result.rule.name}`);
      await refreshModeration();
      setState((current) => ({ ...current, error: '' }));
    } catch (error) {
      setNukeRollbackResult('');
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'nuke rollback failed' }));
    }
  }

  async function handleSaveCounter() {
    try { await saveBotCounter(counterDraft, apiBaseUrl, operatorSecret); setCounterDraft(defaultCounterDraft); await refreshCommunity(); }
    catch (error) { setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'counter save failed' })); }
  }

  async function handleDeleteCounter(counter: BotCounter) {
    try { await deleteBotCounter(counter.id, apiBaseUrl, operatorSecret); await refreshCommunity(); }
    catch (error) { setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'counter delete failed' })); }
  }

  async function handleSaveQuote() {
    try { await saveBotQuote(quoteDraft, apiBaseUrl, operatorSecret); setQuoteDraft(defaultQuoteDraft); await refreshCommunity(); }
    catch (error) { setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'quote save failed' })); }
  }

  async function handleDeleteQuote(quote: BotQuote) {
    try { await deleteBotQuote(quote.id, apiBaseUrl, operatorSecret); await refreshCommunity(); }
    catch (error) { setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'quote delete failed' })); }
  }

  async function handleSaveAPIVariable() {
    try { await saveBotAPIVariable(apiVariableDraft, apiBaseUrl, operatorSecret); setAPIVariableDraft(defaultAPIVariableDraft); await refreshCommunity(); }
    catch (error) { setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'api variable save failed' })); }
  }

  async function handleDeleteAPIVariable(variable: BotAPIVariable) {
    try { await deleteBotAPIVariable(variable.id, apiBaseUrl, operatorSecret); await refreshCommunity(); }
    catch (error) { setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'api variable delete failed' })); }
  }

  async function handleSaveCommand() {
    setSavingCommand(true);
    setState((current) => ({ ...current, error: '' }));
    try {
      await saveBotCommand(commandDraft, apiBaseUrl, operatorSecret);
      setCommandDraft(defaultCommandDraft);
      await refreshCommands();
    } catch (error) {
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'command save failed' }));
    } finally {
      setSavingCommand(false);
    }
  }

  async function handleToggleCommand(command: BotCommand) {
    setState((current) => ({ ...current, error: '' }));
    try {
      await setBotCommandEnabled(command.id, !command.enabled, apiBaseUrl, operatorSecret);
      await refreshCommands();
    } catch (error) {
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'command update failed' }));
    }
  }

  async function handleDeleteCommand(command: BotCommand) {
    setState((current) => ({ ...current, error: '' }));
    try {
      await deleteBotCommand(command.id, apiBaseUrl, operatorSecret);
      await refreshCommands();
    } catch (error) {
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'command delete failed' }));
    }
  }

  async function handleSaveTimer() {
    setSavingTimer(true);
    setState((current) => ({ ...current, error: '' }));
    try {
      await saveBotTimer(draft, apiBaseUrl, operatorSecret);
      setDraft(defaultTimerDraft);
      await refreshTimers();
    } catch (error) {
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'timer save failed' }));
    } finally {
      setSavingTimer(false);
    }
  }

  async function handleToggleTimer(timer: BotTimer) {
    setState((current) => ({ ...current, error: '' }));
    try {
      await setBotTimerEnabled(timer.id, !timer.enabled, apiBaseUrl, operatorSecret);
      await refreshTimers();
    } catch (error) {
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'timer update failed' }));
    }
  }

  async function handleRunTimer(timer: BotTimer) {
    setRunningTimer(timer.id);
    setTimerRunResult('');
    setState((current) => ({ ...current, error: '' }));
    try {
      const result = await runBotTimer(timer.id, apiBaseUrl, operatorSecret);
      setTimerRunResult(`Sent ${timer.name}. Event ${result.eventId}. Schedule unchanged.`);
      const audit = await fetchBotAudit(25, apiBaseUrl, operatorSecret);
      setState((current) => ({ ...current, audit }));
    } catch (error) {
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'timer run failed' }));
    } finally {
      setRunningTimer(null);
    }
  }

  async function handleDeleteTimer(timer: BotTimer) {
    setState((current) => ({ ...current, error: '' }));
    try {
      await deleteBotTimer(timer.id, apiBaseUrl, operatorSecret);
      await refreshTimers();
    } catch (error) {
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'timer delete failed' }));
    }
  }

  return (
    <>
      {state.error ? (
        <Notice tone="bad" role="alert">
          {state.error}
        </Notice>
      ) : null}

      <BotStatusSummary status={state.status} audit={state.audit} />

      <CustomizablePanelLayout
        page="bot"
        items={config.panelLayouts.bot}
        onChange={updateBotPanelLayout}
        description="Reorder or hide safe bot panels. Moderation guardrails stay locked."
        panels={[
          {
            id: 'commands',
            node: (
              <BotCommandsPanel
                draft={commandDraft}
                commands={state.commands}
                saving={savingCommand}
                onDraftChange={setCommandDraft}
                onSave={handleSaveCommand}
                onToggle={handleToggleCommand}
                onDelete={handleDeleteCommand}
              />
            ),
          },
          {
            id: 'community',
            node: (
              <BotCommunityPanel
                counters={state.counters}
                quotes={state.quotes}
                counterDraft={counterDraft}
                quoteDraft={quoteDraft}
                onCounterDraftChange={setCounterDraft}
                onQuoteDraftChange={setQuoteDraft}
                onSaveCounter={handleSaveCounter}
                onDeleteCounter={handleDeleteCounter}
                onSaveQuote={handleSaveQuote}
                onDeleteQuote={handleDeleteQuote}
              />
            ),
          },
          {
            id: 'api-variables',
            node: (
              <BotAPIVariablesPanel
                variables={state.apiVariables}
                draft={apiVariableDraft}
                onDraftChange={setAPIVariableDraft}
                onSave={handleSaveAPIVariable}
                onDelete={handleDeleteAPIVariable}
              />
            ),
          },
          {
            id: 'timers',
            node: (
              <BotTimersPanel
                draft={draft}
                timers={state.timers}
                saving={savingTimer}
                runningId={runningTimer}
                runResult={timerRunResult}
                onRun={handleRunTimer}
                onDraftChange={setDraft}
                onSave={handleSaveTimer}
                onToggle={handleToggleTimer}
                onDelete={handleDeleteTimer}
              />
            ),
          },
        ]}
      />

      <BotModerationPanel
        rules={state.moderation}
        draft={moderationDraft}
        test={moderationTest}
        dryRun={state.moderationDryRun}
        actionDraft={moderationAction}
        actionResult={moderationActionResult}
        nukeDraft={nukeDraft}
        nukeResult={nukeResult}
        nukeExecuteResult={nukeExecuteResult}
        nukeRollbackResult={nukeRollbackResult}
        onDraftChange={setModerationDraft}
        onTestChange={setModerationTest}
        onActionChange={setModerationAction}
        onNukeDraftChange={setNukeDraft}
        onSave={handleSaveModerationRule}
        onDelete={handleDeleteModerationRule}
        onDryRun={handleModerationDryRun}
        onAction={handleModerationAction}
        onNukeDryRun={handleNukeDryRun}
        onNukeExecute={handleNukeExecute}
        onNukeRollback={handleNukeRollback}
      />
    </>
  );
}

