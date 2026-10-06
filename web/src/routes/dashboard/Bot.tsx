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
  type BotAPIVariablesResponse,
  type BotCommandsResponse,
  type BotCountersResponse,
  type BotModerationDryRunResponse,
  type BotModerationRulesResponse,
  type BotNukeDryRunResponse,
  type BotNukeExecuteResponse,
  type BotQuotesResponse,
  type BotTimersResponse,
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
  type APIVariableDraft,
  type BotPageState,
  type CommandDraft,
  type CounterDraft,
  type ModerationActionDraft,
  type ModerationDraft,
  type ModerationTestDraft,
  type NukeDraft,
  type QuoteDraft,
  type TimerDraft,
} from './bot/types';
import { renderCommandPreview } from './bot/renderCommandPreview';
import { CustomizablePanelLayout } from './components/CustomizablePanelLayout';
import type { PanelLayoutItem } from '../../lib/dashboardLayout';
import { loadDashboardConfig, saveDashboardConfig } from '../../lib/dashboardConfig';
import { Check, Empty, Field, Notice, Panel, SubPanel, Tag, type Tone } from './ui';

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
    const [status, timers] = await Promise.all([fetchBotStatus(apiBaseUrl, operatorSecret), fetchBotTimers(apiBaseUrl, operatorSecret)]);
    setState((current) => ({ ...current, status, timers }));
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

function enabledTone(enabled: boolean): Tone {
  return enabled ? 'ok' : 'neutral';
}

function BotCommandsPanel({
  draft,
  commands,
  saving,
  onDraftChange,
  onSave,
  onToggle,
  onDelete,
}: {
  draft: CommandDraft;
  commands: BotCommandsResponse | null;
  saving: boolean;
  onDraftChange: (draft: CommandDraft) => void;
  onSave: () => void;
  onToggle: (command: BotCommand) => void;
  onDelete: (command: BotCommand) => void;
}) {
  const items = commands?.commands ?? [];
  const canSave = draft.name.trim() !== '' && draft.response.trim() !== '' && draft.cooldownSeconds >= 0;
  return (
    <Panel
      id="bot-commands"
      kicker="Chat commands"
      title="Bot commands"
      description="Create safe Twitch chat responses. Custom commands can publish ticker/alert overlays, send guarded Twitch chat replies, or both. They do not run scripts, call arbitrary APIs, or trigger OBS actions."
    >
      {commands?.loadError ? <Notice tone="warn">Commands unavailable: {commands.loadError}</Notice> : null}

      <div className="grid gap-3 md:grid-cols-[1fr_8rem_8rem_10rem_8rem]">
        <Field label="Command name">
          <input className="desk-input desk-input--mono" value={draft.name} onChange={(event) => onDraftChange({ ...draft, name: event.target.value })} placeholder="discord" />
        </Field>
        <Field label="Response mode">
          <select className="desk-input" value={draft.responseMode} onChange={(event) => onDraftChange({ ...draft, responseMode: event.target.value as CommandDraft['responseMode'] })}>
            <option value="overlay">Overlay</option>
            <option value="chat">Chat</option>
            <option value="both">Both</option>
          </select>
        </Field>
        <Field label="Action">
          <select className="desk-input" value={draft.action} onChange={(event) => onDraftChange({ ...draft, action: event.target.value as CommandDraft['action'] })}>
            <option value="ticker">Ticker</option>
            <option value="alert">Alert</option>
          </select>
        </Field>
        <Field label="Required role">
          <select className="desk-input" value={draft.roleRequired} onChange={(event) => onDraftChange({ ...draft, roleRequired: event.target.value as CommandDraft['roleRequired'] })}>
            <option value="viewer">Viewer</option>
            <option value="moderator">Moderator</option>
            <option value="broadcaster">Broadcaster</option>
          </select>
        </Field>
        <Field label="Cooldown seconds">
          <input className="desk-input" type="number" min={0} value={draft.cooldownSeconds} onChange={(event) => onDraftChange({ ...draft, cooldownSeconds: Number(event.target.value) })} />
        </Field>
      </div>
      <Field label="Response">
        <textarea className="desk-input" value={draft.response} onChange={(event) => onDraftChange({ ...draft, response: event.target.value })} placeholder="Join the Discord: https://example.test" />
      </Field>
      <SubPanel title="Preview with sample context">
        <p className="mono">{renderCommandPreview(draft) || 'Enter a response to preview rendered output.'}</p>
      </SubPanel>
      <div className="flex flex-wrap items-center gap-3">
        <Check label="Enabled" checked={draft.enabled} onChange={(enabled) => onDraftChange({ ...draft, enabled })} />
        <button className="desk-btn desk-btn--primary" type="button" disabled={!canSave || saving} onClick={onSave}>
          {saving ? 'Saving…' : 'Save command'}
        </button>
      </div>

      {items.length === 0 ? (
        <Empty>No bot commands configured yet.</Empty>
      ) : (
        <ul className="desk-rows">
          {items.map((command) => (
            <li key={command.id} className="desk-row">
              <div className="desk-row__line">
                <p className="desk-row__title mono">!{command.name}</p>
                <Tag tone={enabledTone(command.enabled)}>{command.enabled ? 'enabled' : 'paused'}</Tag>
              </div>
              <p className="desk-row__meta">
                {command.action} · {command.responseMode || 'overlay'} · {command.roleRequired}+ · {command.cooldownSeconds}s cooldown
              </p>
              <p className="desk-row__text">{command.response}</p>
              <div className="flex flex-wrap gap-2 pt-1">
                <button className="desk-btn desk-btn--small" type="button" onClick={() => onToggle(command)}>
                  {command.enabled ? 'Pause' : 'Enable'}
                </button>
                <button className="desk-btn desk-btn--small desk-btn--danger" type="button" onClick={() => onDelete(command)}>
                  Delete
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

function BotCommunityPanel({
  counters,
  quotes,
  counterDraft,
  quoteDraft,
  onCounterDraftChange,
  onQuoteDraftChange,
  onSaveCounter,
  onDeleteCounter,
  onSaveQuote,
  onDeleteQuote,
}: {
  counters: BotCountersResponse | null;
  quotes: BotQuotesResponse | null;
  counterDraft: CounterDraft;
  quoteDraft: QuoteDraft;
  onCounterDraftChange: (draft: CounterDraft) => void;
  onQuoteDraftChange: (draft: QuoteDraft) => void;
  onSaveCounter: () => void;
  onDeleteCounter: (counter: BotCounter) => void;
  onSaveQuote: () => void;
  onDeleteQuote: (quote: BotQuote) => void;
}) {
  const counterItems = counters?.counters ?? [];
  const quoteItems = quotes?.quotes ?? [];
  return (
    <Panel
      id="bot-community"
      kicker="Community"
      title="Bot counters and quotes"
      description={
        <>
          Community tools for <code>!count name</code>, <code>!quote</code>, and moderator <code>!quote add text</code>.
        </>
      }
    >
      <div className="grid gap-4 lg:grid-cols-2">
        <SubPanel title="Counters">
          <div className="grid gap-3 md:grid-cols-[1fr_7rem]">
            <Field label="Counter name">
              <input className="desk-input desk-input--mono" value={counterDraft.name} onChange={(event) => onCounterDraftChange({ ...counterDraft, name: event.target.value })} placeholder="hype" />
            </Field>
            <Field label="Value">
              <input className="desk-input" type="number" value={counterDraft.value} onChange={(event) => onCounterDraftChange({ ...counterDraft, value: Number(event.target.value) })} />
            </Field>
          </div>
          <div>
            <button className="desk-btn desk-btn--primary" type="button" disabled={counterDraft.name.trim() === ''} onClick={onSaveCounter}>
              Save counter
            </button>
          </div>
          {counterItems.length === 0 ? (
            <Empty>No counters yet.</Empty>
          ) : (
            <ul className="desk-rows">
              {counterItems.map((counter) => (
                <li key={counter.id} className="desk-row">
                  <div className="desk-row__line">
                    <p className="desk-row__title">
                      <span className="mono">{counter.name}</span> · {counter.value}
                    </p>
                    <button className="desk-btn desk-btn--small desk-btn--danger" type="button" onClick={() => onDeleteCounter(counter)}>
                      Delete
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </SubPanel>

        <SubPanel title="Quotes">
          <Field label="Author" hint="Optional.">
            <input className="desk-input" value={quoteDraft.author} onChange={(event) => onQuoteDraftChange({ ...quoteDraft, author: event.target.value })} placeholder="author optional" />
          </Field>
          <Field label="Quote text">
            <textarea className="desk-input" value={quoteDraft.text} onChange={(event) => onQuoteDraftChange({ ...quoteDraft, text: event.target.value })} placeholder="quote text" />
          </Field>
          <div>
            <button className="desk-btn desk-btn--primary" type="button" disabled={quoteDraft.text.trim() === ''} onClick={onSaveQuote}>
              Save quote
            </button>
          </div>
          {quoteItems.length === 0 ? (
            <Empty>No quotes yet.</Empty>
          ) : (
            <ul className="desk-rows">
              {quoteItems.map((quote) => (
                <li key={quote.id} className="desk-row">
                  <p className="desk-row__text">{quote.text}</p>
                  <div className="desk-row__line">
                    <span className="desk-row__meta">{quote.author || 'unknown'}</span>
                    <button className="desk-btn desk-btn--small desk-btn--danger" type="button" onClick={() => onDeleteQuote(quote)}>
                      Delete
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </SubPanel>
      </div>
    </Panel>
  );
}

function BotAPIVariablesPanel({ variables, draft, onDraftChange, onSave, onDelete }: { variables: BotAPIVariablesResponse | null; draft: APIVariableDraft; onDraftChange: (draft: APIVariableDraft) => void; onSave: () => void; onDelete: (variable: BotAPIVariable) => void }) {
  const items = variables?.variables ?? [];
  const canSave = draft.name.trim() !== '' && draft.url.trim().startsWith('https://');
  return (
    <Panel
      id="bot-api-variables"
      kicker="Templates"
      title="Bot API variables"
      description={
        <>
          Operator-defined read-only HTTP variables for templates like <code>{'{api:weather}'}</code>. GET-only, HTTPS-only, capped, cached, and blocked from private hosts.
        </>
      }
    >
      {variables?.loadError ? <Notice tone="warn">API variables unavailable: {variables.loadError}</Notice> : null}
      <div className="grid gap-3 md:grid-cols-[1fr_2fr_10rem_8rem]">
        <Field label="Name">
          <input className="desk-input desk-input--mono" value={draft.name} onChange={(event) => onDraftChange({ ...draft, name: event.target.value })} placeholder="weather" />
        </Field>
        <Field label="HTTPS URL">
          <input className="desk-input desk-input--mono" value={draft.url} onChange={(event) => onDraftChange({ ...draft, url: event.target.value })} placeholder="https://api.example.test/value" />
        </Field>
        <Field label="JSON selector">
          <input className="desk-input desk-input--mono" value={draft.responseSelector} onChange={(event) => onDraftChange({ ...draft, responseSelector: event.target.value })} placeholder="data.value" />
        </Field>
        <Field label="TTL seconds">
          <input className="desk-input" type="number" min={15} max={3600} value={draft.cacheTtlSeconds} onChange={(event) => onDraftChange({ ...draft, cacheTtlSeconds: Number(event.target.value) })} />
        </Field>
      </div>
      <div className="grid gap-3 md:grid-cols-[1fr_8rem]">
        <Field label="Fallback">
          <input className="desk-input" value={draft.fallback} onChange={(event) => onDraftChange({ ...draft, fallback: event.target.value })} placeholder="unavailable" />
        </Field>
        <Field label="Timeout ms">
          <input className="desk-input" type="number" min={250} max={3000} value={draft.timeoutMs} onChange={(event) => onDraftChange({ ...draft, timeoutMs: Number(event.target.value) })} />
        </Field>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <Check label="Enabled" checked={draft.enabled} onChange={(enabled) => onDraftChange({ ...draft, enabled })} />
        <button className="desk-btn desk-btn--primary" type="button" disabled={!canSave} onClick={onSave}>
          Save API variable
        </button>
      </div>
      {items.length === 0 ? (
        <Empty>No API variables configured yet.</Empty>
      ) : (
        <ul className="desk-rows">
          {items.map((variable) => (
            <li key={variable.id} className={`desk-row ${variable.lastError ? 'desk-row--warn' : ''}`}>
              <div className="desk-row__line">
                <p className="desk-row__title mono">{`{api:${variable.name}}`}</p>
                <Tag tone={enabledTone(variable.enabled)}>{variable.enabled ? 'enabled' : 'paused'}</Tag>
              </div>
              <p className="desk-row__text mono">{variable.url}</p>
              <p className="desk-row__meta">
                status {variable.lastStatus || 'untested'} · ttl {variable.cacheTtlSeconds}s · timeout {variable.timeoutMs}ms
              </p>
              {variable.lastError ? <p className="desk-row__text">{variable.lastError}</p> : null}
              <div className="flex flex-wrap gap-2 pt-1">
                <button className="desk-btn desk-btn--small desk-btn--danger" type="button" onClick={() => onDelete(variable)}>
                  Delete
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

function ruleState(rule: BotModerationRule): { label: string; tone: Tone } {
  if (rule.expiresAt && new Date(rule.expiresAt).getTime() <= Date.now()) return { label: 'expired', tone: 'neutral' };
  if (!rule.enabled) return { label: 'paused', tone: 'neutral' };
  return rule.enforce ? { label: 'enforced', tone: 'warn' } : { label: 'dry-run', tone: 'ok' };
}

function BotModerationPanel({
  rules,
  draft,
  test,
  dryRun,
  actionDraft,
  actionResult,
  nukeDraft,
  nukeResult,
  nukeExecuteResult,
  nukeRollbackResult,
  onDraftChange,
  onTestChange,
  onActionChange,
  onNukeDraftChange,
  onSave,
  onDelete,
  onDryRun,
  onAction,
  onNukeDryRun,
  onNukeExecute,
  onNukeRollback,
}: {
  rules: BotModerationRulesResponse | null;
  draft: ModerationDraft;
  test: ModerationTestDraft;
  dryRun: BotModerationDryRunResponse | null;
  actionDraft: ModerationActionDraft;
  actionResult: string;
  nukeDraft: NukeDraft;
  nukeResult: BotNukeDryRunResponse | null;
  nukeExecuteResult: BotNukeExecuteResponse | null;
  nukeRollbackResult: string;
  onDraftChange: (draft: ModerationDraft) => void;
  onTestChange: (test: ModerationTestDraft) => void;
  onActionChange: (draft: ModerationActionDraft) => void;
  onNukeDraftChange: (draft: NukeDraft) => void;
  onSave: () => void;
  onDelete: (rule: BotModerationRule) => void;
  onDryRun: () => void;
  onAction: () => void;
  onNukeDryRun: () => void;
  onNukeExecute: () => void;
  onNukeRollback: (rule: BotModerationRule) => void;
}) {
  const items = rules?.rules ?? [];
  const canSave = draft.name.trim() !== '' && draft.pattern.trim() !== '';
  const canExecuteNuke = Boolean(nukeResult && nukeResult.matches.length > 0 && nukeDraft.confirm);
  const activeRadiationRules = items.filter((rule) => rule.enabled && rule.source === 'nuke_radiation' && (!rule.expiresAt || new Date(rule.expiresAt).getTime() > Date.now()));
  return (
    <>
      <Panel
        id="bot-moderation"
        kicker="Moderation"
        title="Bot moderation dry-run"
        description="Blocked-term rules are stored and testable here. Rules stay dry-run unless enforce is checked. Enforced delete, timeout, and ban actions require Twitch moderation scopes."
      >
        {rules?.loadError ? <Notice tone="warn">Moderation unavailable: {rules.loadError}</Notice> : null}

        <SubPanel title="New rule">
          <div className="grid gap-3 md:grid-cols-[1fr_1fr_9rem_8rem]">
            <Field label="Rule name">
              <input className="desk-input desk-input--mono" value={draft.name} onChange={(event) => onDraftChange({ ...draft, name: event.target.value })} placeholder="spam_link" />
            </Field>
            <Field label="Pattern">
              <input className="desk-input desk-input--mono" value={draft.pattern} onChange={(event) => onDraftChange({ ...draft, pattern: event.target.value })} placeholder="buy now" />
            </Field>
            <Field label="Match target">
              <select className="desk-input" value={draft.matchTarget} onChange={(event) => onDraftChange({ ...draft, matchTarget: event.target.value as ModerationDraft['matchTarget'] })}>
                <option value="message">Message</option>
                <option value="username">Username</option>
                <option value="both">Both</option>
              </select>
            </Field>
            <Field label="Action">
              <select className="desk-input" value={draft.action} onChange={(event) => onDraftChange({ ...draft, action: event.target.value as ModerationDraft['action'] })}>
                <option value="flag">Flag</option>
                <option value="delete">Delete label</option>
                <option value="timeout">Timeout label</option>
                <option value="ban">Ban label</option>
              </select>
            </Field>
          </div>
          <Field label="Reason" hint="Shown in audit and planning.">
            <input className="desk-input" value={draft.reason} onChange={(event) => onDraftChange({ ...draft, reason: event.target.value })} placeholder="reason shown in audit/planning" />
          </Field>
          <div className="flex flex-wrap items-center gap-3">
            <Check label="Enabled" checked={draft.enabled} onChange={(enabled) => onDraftChange({ ...draft, enabled })} />
            <Check label="Enforce" checked={draft.enforce} onChange={(enforce) => onDraftChange({ ...draft, enforce })} />
            <Check label="Regex" checked={draft.regex} onChange={(regex) => onDraftChange({ ...draft, regex })} />
            <Check label="Case sensitive" checked={draft.caseSensitive} onChange={(caseSensitive) => onDraftChange({ ...draft, caseSensitive })} />
            <button className="desk-btn desk-btn--primary" type="button" disabled={!canSave} onClick={onSave}>
              Save rule
            </button>
          </div>
        </SubPanel>

        <SubPanel title="Test a message">
          <div className="grid items-end gap-3 md:grid-cols-[10rem_1fr_auto]">
            <Field label="Username">
              <input className="desk-input" value={test.username} onChange={(event) => onTestChange({ ...test, username: event.target.value })} placeholder="username" />
            </Field>
            <Field label="Message to test">
              <input className="desk-input" value={test.message} onChange={(event) => onTestChange({ ...test, message: event.target.value })} placeholder="message to test" />
            </Field>
            <button className="desk-btn" type="button" onClick={onDryRun}>
              Dry run
            </button>
          </div>
          {dryRun ? (
            <p className="desk-row__text">
              Matches: <span className="mono">{dryRun.matches.length ? dryRun.matches.map((match) => `${match.ruleName}:${match.action}`).join(', ') : 'none'}</span>
            </p>
          ) : null}
        </SubPanel>

        <SubPanel title="Manual Twitch moderation action">
          <p className="desk-row__text">Requires Twitch moderation scopes. Use only after dry-run evidence. Failed and successful attempts are written to bot audit.</p>
          <div className="grid gap-3 md:grid-cols-[9rem_1fr_1fr_8rem]">
            <Field label="Action">
              <select className="desk-input" value={actionDraft.action} onChange={(event) => onActionChange({ ...actionDraft, action: event.target.value as ModerationActionDraft['action'] })}>
                <option value="delete">Delete message</option>
                <option value="timeout">Timeout user</option>
                <option value="ban">Ban user</option>
                <option value="unban">Unban user</option>
              </select>
            </Field>
            <Field label="Target user ID">
              <input className="desk-input desk-input--mono" value={actionDraft.targetUserId} onChange={(event) => onActionChange({ ...actionDraft, targetUserId: event.target.value })} placeholder="target user id" />
            </Field>
            <Field label="Message ID" hint="Needed for delete.">
              <input className="desk-input desk-input--mono" value={actionDraft.messageId} onChange={(event) => onActionChange({ ...actionDraft, messageId: event.target.value })} placeholder="message id for delete" />
            </Field>
            <Field label="Duration seconds">
              <input className="desk-input" type="number" min={1} value={actionDraft.durationSeconds} onChange={(event) => onActionChange({ ...actionDraft, durationSeconds: Number(event.target.value) })} />
            </Field>
          </div>
          <Field label="Reason">
            <input className="desk-input" value={actionDraft.reason} onChange={(event) => onActionChange({ ...actionDraft, reason: event.target.value })} placeholder="reason" />
          </Field>
          <div className="flex flex-wrap items-center gap-3">
            <button className="desk-btn desk-btn--danger" type="button" onClick={onAction}>
              Run guarded action
            </button>
            {actionResult ? <Tag tone="ok">{actionResult}</Tag> : null}
          </div>
        </SubPanel>

        {items.length === 0 ? (
          <Empty>No moderation rules configured yet.</Empty>
        ) : (
          <ul className="desk-rows">
            {items.map((rule) => {
              const stateTag = ruleState(rule);
              return (
                <li key={rule.id} className="desk-row">
                  <div className="desk-row__line">
                    <p className="desk-row__title mono">{rule.name}</p>
                    <Tag tone={stateTag.tone}>{stateTag.label}</Tag>
                  </div>
                  <p className="desk-row__meta">
                    {rule.matchTarget} · {rule.regex ? 'regex' : 'contains'} · {rule.action}
                  </p>
                  <p className="desk-row__text mono">{rule.pattern}</p>
                  <div className="desk-row__line">
                    <span className="desk-row__meta">{rule.reason || 'no reason'}</span>
                    <button className="desk-btn desk-btn--small desk-btn--danger" type="button" onClick={() => onDelete(rule)}>
                      Delete
                    </button>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </Panel>

      <Panel
        danger
        kicker="Destructive"
        title="Nuke dry-run planner"
        description="Dry-run first. Execute requires confirmation, is capped, and only delete and timeout are supported."
      >
        <SubPanel title="1. Plan">
          <div className="grid gap-3 md:grid-cols-[1fr_8rem_8rem]">
            <Field label="Pattern">
              <input className="desk-input desk-input--mono" value={nukeDraft.pattern} onChange={(event) => onNukeDraftChange({ ...nukeDraft, pattern: event.target.value, confirm: false })} placeholder="phrase or regex" />
            </Field>
            <Field label="Lookback minutes">
              <input className="desk-input" type="number" min={1} max={360} value={nukeDraft.lookbackMinutes} onChange={(event) => onNukeDraftChange({ ...nukeDraft, lookbackMinutes: Number(event.target.value), confirm: false })} />
            </Field>
            <Field label="Match limit">
              <input className="desk-input" type="number" min={1} max={250} value={nukeDraft.limit} onChange={(event) => onNukeDraftChange({ ...nukeDraft, limit: Number(event.target.value), confirm: false })} />
            </Field>
          </div>
          <div className="grid gap-3 md:grid-cols-[8rem_8rem_9rem]">
            <Field label="Action">
              <select className="desk-input" value={nukeDraft.action} onChange={(event) => onNukeDraftChange({ ...nukeDraft, action: event.target.value as NukeDraft['action'], confirm: false })}>
                <option value="delete">Delete</option>
                <option value="timeout">Timeout</option>
              </select>
            </Field>
            <Field label="Duration seconds">
              <input className="desk-input" type="number" min={1} value={nukeDraft.durationSeconds} onChange={(event) => onNukeDraftChange({ ...nukeDraft, durationSeconds: Number(event.target.value), confirm: false })} />
            </Field>
            <Field label="Radiation minutes">
              <input className="desk-input" type="number" min={0} max={120} value={nukeDraft.radiationMinutes} onChange={(event) => onNukeDraftChange({ ...nukeDraft, radiationMinutes: Number(event.target.value), confirm: false })} placeholder="radiation min" />
            </Field>
          </div>
          <p className="desk-row__meta">Radiation minutes optionally enforces this pattern for future messages for up to 120 minutes. Future timeout radiation uses the standard 60-second timeout.</p>
          <div className="flex flex-wrap items-center gap-3">
            <Check label="Regex" checked={nukeDraft.regex} onChange={(regex) => onNukeDraftChange({ ...nukeDraft, regex, confirm: false })} />
            <Check label="Case sensitive" checked={nukeDraft.caseSensitive} onChange={(caseSensitive) => onNukeDraftChange({ ...nukeDraft, caseSensitive, confirm: false })} />
            <button className="desk-btn" type="button" disabled={nukeDraft.pattern.trim() === ''} onClick={onNukeDryRun}>
              Plan nuke
            </button>
          </div>
          {nukeResult ? (
            <>
              <p className="desk-row__text">Planned matches: {nukeResult.matches.length}</p>
              {nukeResult.matches.length > 0 ? (
                <ul className="desk-rows">
                  {nukeResult.matches.slice(0, 5).map((match) => (
                    <li key={match.eventId} className="desk-row">
                      <div className="desk-row__line">
                        <span className="desk-row__title mono">{match.user}</span>
                      </div>
                      <p className="desk-row__text">{match.message}</p>
                    </li>
                  ))}
                </ul>
              ) : null}
            </>
          ) : null}
        </SubPanel>

        <SubPanel title="2. Review">
          <Check label="I reviewed this dry-run plan" checked={nukeDraft.confirm} onChange={(confirm) => onNukeDraftChange({ ...nukeDraft, confirm })} />
        </SubPanel>

        <SubPanel title="3. Execute">
          <div>
            <button className="desk-btn desk-btn--danger" type="button" disabled={!canExecuteNuke} onClick={onNukeExecute}>
              Execute planned nuke
            </button>
          </div>
          {nukeExecuteResult ? (
            <Notice tone="warn">
              Executed {nukeExecuteResult.action}: {nukeExecuteResult.succeeded}/{nukeExecuteResult.attempted} succeeded · {nukeExecuteResult.failed} failed
              {nukeExecuteResult.radiationRule ? ` · radiation until ${nukeExecuteResult.radiationRule.expiresAt ?? 'expiry'}` : ''}
            </Notice>
          ) : null}
          {nukeRollbackResult ? <Notice tone="ok">Rollback: {nukeRollbackResult}</Notice> : null}
        </SubPanel>

        {activeRadiationRules.length > 0 ? (
          <SubPanel title="Active radiation windows">
            <ul className="desk-rows">
              {activeRadiationRules.map((rule) => (
                <li key={rule.id} className="desk-row desk-row--warn">
                  <div className="desk-row__line">
                    <span className="desk-row__text">
                      <span className="mono">{rule.pattern}</span> · {rule.action} until {rule.expiresAt}
                    </span>
                    <button className="desk-btn desk-btn--small" type="button" onClick={() => onNukeRollback(rule)}>
                      Rollback window
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          </SubPanel>
        ) : null}
      </Panel>
    </>
  );
}

function BotTimersPanel({
  draft,
  timers,
  saving,
  runningId,
  runResult,
  onRun,
  onDraftChange,
  onSave,
  onToggle,
  onDelete,
}: {
  draft: TimerDraft;
  timers: BotTimersResponse | null;
  saving: boolean;
  runningId: string | null;
  runResult: string;
  onRun: (timer: BotTimer) => void;
  onDraftChange: (draft: TimerDraft) => void;
  onSave: () => void;
  onToggle: (timer: BotTimer) => void;
  onDelete: (timer: BotTimer) => void;
}) {
  const items = timers?.timers ?? [];
  const canSave = draft.name.trim() !== '' && draft.response.trim() !== '' && draft.intervalSeconds >= 30;
  return (
    <Panel
      id="bot-timers"
      kicker="Schedule"
      title="Bot timers"
      description="Enabled timers send ticker or alert events when due. Run once rehearses any saved timer without changing its schedule."
    >
      {timers?.loadError ? <Notice tone="warn">Timers unavailable: {timers.loadError}</Notice> : null}

      <div className="grid gap-3 md:grid-cols-[1fr_9rem_8rem]">
        <Field label="Timer name">
          <input className="desk-input desk-input--mono" value={draft.name} onChange={(event) => onDraftChange({ ...draft, name: event.target.value })} placeholder="stream_hype" />
        </Field>
        <Field label="Interval seconds">
          <input className="desk-input" type="number" min={30} value={draft.intervalSeconds} onChange={(event) => onDraftChange({ ...draft, intervalSeconds: Number(event.target.value) })} />
        </Field>
        <Field label="Action">
          <select className="desk-input" value={draft.action} onChange={(event) => onDraftChange({ ...draft, action: event.target.value as TimerDraft['action'] })}>
            <option value="ticker">Ticker</option>
            <option value="alert">Alert</option>
          </select>
        </Field>
      </div>
      <Field label="Response">
        <textarea className="desk-input" value={draft.response} onChange={(event) => onDraftChange({ ...draft, response: event.target.value })} placeholder="Hydrate, stretch, and check posture." />
      </Field>
      <div className="flex flex-wrap items-center gap-3">
        <Check label="Enabled" checked={draft.enabled} onChange={(enabled) => onDraftChange({ ...draft, enabled })} />
        <button className="desk-btn desk-btn--primary" type="button" disabled={!canSave || saving} onClick={onSave}>
          {saving ? 'Saving…' : 'Save timer'}
        </button>
      </div>

      {runResult ? <Notice tone="ok">{runResult}</Notice> : null}

      {items.length === 0 ? (
        <Empty>No bot timers configured yet.</Empty>
      ) : (
        <ul className="desk-rows">
          {items.map((timer) => (
            <li key={timer.id} className="desk-row">
              <div className="desk-row__line">
                <p className="desk-row__title mono">{timer.name}</p>
                <Tag tone={enabledTone(timer.enabled)}>{timer.enabled ? 'enabled' : 'paused'}</Tag>
              </div>
              <p className="desk-row__meta">
                every {timer.intervalSeconds}s · {timer.action}
              </p>
              <p className="desk-row__text">{timer.response}</p>
              <p className="desk-row__meta">Last scheduled run: {timer.lastRunAt ? new Date(timer.lastRunAt).toLocaleString() : 'Never'}</p>
              <div className="flex flex-wrap gap-2 pt-1">
                <button className="desk-btn desk-btn--small" type="button" disabled={runningId !== null} onClick={() => onRun(timer)}>
                  {runningId === timer.id ? 'Running…' : 'Run once'}
                </button>
                <button className="desk-btn desk-btn--small" type="button" onClick={() => onToggle(timer)}>
                  {timer.enabled ? 'Pause' : 'Enable'}
                </button>
                <button className="desk-btn desk-btn--small desk-btn--danger" type="button" onClick={() => onDelete(timer)}>
                  Delete
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}
