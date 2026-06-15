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
  executeBotModerationAction,
  fetchBotAudit,
  fetchBotAPIVariables,
  fetchBotCommands,
  fetchBotCounters,
  fetchBotModerationRules,
  fetchBotQuotes,
  fetchBotStatus,
  fetchBotTimers,
  saveBotCommand,
  saveBotCounter,
  saveBotAPIVariable,
  saveBotModerationRule,
  saveBotQuote,
  saveBotTimer,
  setBotCommandEnabled,
  setBotTimerEnabled,
  type BotAuditResponse,
  type BotAPIVariable,
  type BotAPIVariablesResponse,
  type BotCommand,
  type BotCommandsResponse,
  type BotCounter,
  type BotCountersResponse,
  type BotModerationDryRunResponse,
  type BotModerationRule,
  type BotModerationRulesResponse,
  type BotNukeDryRunResponse,
  type BotQuote,
  type BotQuotesResponse,
  type BotStatus,
  type BotTimer,
  type BotTimersResponse,
} from '../../lib/api';
import { loadOperatorSecret } from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { DashboardShell } from './DashboardShell';

type BotPageState = {
  status: BotStatus | null;
  audit: BotAuditResponse | null;
  commands: BotCommandsResponse | null;
  counters: BotCountersResponse | null;
  quotes: BotQuotesResponse | null;
  apiVariables: BotAPIVariablesResponse | null;
  moderation: BotModerationRulesResponse | null;
  moderationDryRun: BotModerationDryRunResponse | null;
  timers: BotTimersResponse | null;
  loading: boolean;
  error: string;
};

type CommandDraft = {
  name: string;
  response: string;
  action: 'ticker' | 'alert';
  responseMode: 'overlay' | 'chat' | 'both';
  roleRequired: 'viewer' | 'moderator' | 'broadcaster';
  cooldownSeconds: number;
  enabled: boolean;
};

type TimerDraft = {
  name: string;
  intervalSeconds: number;
  response: string;
  action: 'ticker' | 'alert';
  enabled: boolean;
};

type ModerationDraft = {
  name: string;
  enabled: boolean;
  pattern: string;
  matchTarget: 'message' | 'username' | 'both';
  caseSensitive: boolean;
  regex: boolean;
  action: 'flag' | 'delete' | 'timeout' | 'ban';
  reason: string;
};

const defaultTimerDraft: TimerDraft = { name: '', intervalSeconds: 300, response: '', action: 'ticker', enabled: true };
const defaultCommandDraft: CommandDraft = { name: '', response: '', action: 'ticker', responseMode: 'overlay', roleRequired: 'viewer', cooldownSeconds: 10, enabled: true };
const defaultCounterDraft = { name: '', value: 0 };
const defaultQuoteDraft = { text: '', author: '' };
const defaultAPIVariableDraft = { name: '', enabled: true, url: '', responseSelector: '', fallback: '', cacheTtlSeconds: 60, timeoutMs: 1500 };
const defaultModerationDraft: ModerationDraft = { name: '', enabled: true, pattern: '', matchTarget: 'message', caseSensitive: false, regex: false, action: 'flag', reason: '' };
const defaultModerationTest = { username: '', message: '' };
const defaultModerationAction = { action: 'timeout' as const, targetUserId: '', targetUserLogin: '', messageId: '', durationSeconds: 60, reason: '' };
const defaultNukeDraft = { pattern: '', regex: false, caseSensitive: false, lookbackMinutes: 30, limit: 100 };

export function DashboardBotPage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const operatorSecret = loadOperatorSecret();
  const [state, setState] = useState<BotPageState>({ status: null, audit: null, commands: null, counters: null, quotes: null, apiVariables: null, moderation: null, moderationDryRun: null, timers: null, loading: false, error: '' });
  const [commandDraft, setCommandDraft] = useState<CommandDraft>(defaultCommandDraft);
  const [draft, setDraft] = useState<TimerDraft>(defaultTimerDraft);
  const [savingCommand, setSavingCommand] = useState(false);
  const [savingTimer, setSavingTimer] = useState(false);
  const [counterDraft, setCounterDraft] = useState(defaultCounterDraft);
  const [quoteDraft, setQuoteDraft] = useState(defaultQuoteDraft);
  const [apiVariableDraft, setAPIVariableDraft] = useState(defaultAPIVariableDraft);
  const [moderationDraft, setModerationDraft] = useState<ModerationDraft>(defaultModerationDraft);
  const [moderationTest, setModerationTest] = useState(defaultModerationTest);
  const [moderationAction, setModerationAction] = useState(defaultModerationAction);
  const [moderationActionResult, setModerationActionResult] = useState('');
  const [nukeDraft, setNukeDraft] = useState(defaultNukeDraft);
  const [nukeResult, setNukeResult] = useState<BotNukeDryRunResponse | null>(null);

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
      setState((current) => ({ ...current, error: '' }));
    } catch (error) {
      setNukeResult(null);
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'nuke dry-run failed' }));
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
    <DashboardShell activePath="/dashboard/bot">
      <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
        <p className="hud-label">Bot operations</p>
        <h2 className="hud-title hud-title--compact mt-1">FossaBot parity spine</h2>
        <p className="mt-2 max-w-3xl text-sm text-zinc-300">Bot audit is the first safety layer for future timers, custom commands, counters, quotes, channel-point workflows, and moderation tools.</p>
        {state.loading ? <p className="mt-2 text-xs text-zinc-400">Loading bot status…</p> : null}
        {state.error ? <p className="mt-2 border border-red-400/40 bg-black px-2 py-1 text-xs text-rose-200">{state.error}</p> : null}
      </section>

      <section className="grid gap-3 lg:grid-cols-[0.9fr_1.1fr]">
        <BotStatusPanel status={state.status} />
        <BotAuditPanel audit={state.audit} />
      </section>

      <BotCommandsPanel
        draft={commandDraft}
        commands={state.commands}
        saving={savingCommand}
        onDraftChange={setCommandDraft}
        onSave={handleSaveCommand}
        onToggle={handleToggleCommand}
        onDelete={handleDeleteCommand}
      />

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

      <BotAPIVariablesPanel
        variables={state.apiVariables}
        draft={apiVariableDraft}
        onDraftChange={setAPIVariableDraft}
        onSave={handleSaveAPIVariable}
        onDelete={handleDeleteAPIVariable}
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
        onDraftChange={setModerationDraft}
        onTestChange={setModerationTest}
        onActionChange={setModerationAction}
        onNukeDraftChange={setNukeDraft}
        onSave={handleSaveModerationRule}
        onDelete={handleDeleteModerationRule}
        onDryRun={handleModerationDryRun}
        onAction={handleModerationAction}
        onNukeDryRun={handleNukeDryRun}
      />

      <BotTimersPanel
        draft={draft}
        timers={state.timers}
        saving={savingTimer}
        onDraftChange={setDraft}
        onSave={handleSaveTimer}
        onToggle={handleToggleTimer}
        onDelete={handleDeleteTimer}
      />
    </DashboardShell>
  );
}

function BotStatusPanel({ status }: { status: BotStatus | null }) {
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Bot runtime</p>
      <div className="mt-2 grid gap-1">
        <Line label="storage" value={status ? (status.available ? 'available' : 'unavailable') : 'unknown'} />
        <Line label="commands" value={String(status?.commandCount ?? 0)} />
        <Line label="timers" value={String(status?.timerCount ?? 0)} />
        <Line label="counters" value={String(status?.counterCount ?? 0)} />
        <Line label="quotes" value={String(status?.quoteCount ?? 0)} />
        <Line label="audit events" value={String(status?.auditCount ?? 0)} />
      </div>
      {status?.loadError ? <p className="mt-2 border border-amber-400/40 bg-black px-2 py-1 text-xs text-amber-100">load error :: {status.loadError}</p> : null}
      {status?.lastAuditError ? <p className="mt-2 border border-red-400/40 bg-black px-2 py-1 text-xs text-rose-200">audit error :: {status.lastAuditError}</p> : null}
    </section>
  );
}

function BotAuditPanel({ audit }: { audit: BotAuditResponse | null }) {
  const events = audit?.events ?? [];
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Bot audit</p>
      {audit?.loadError ? <p className="mt-2 border border-amber-400/40 bg-black px-2 py-1 text-xs text-amber-100">audit unavailable :: {audit.loadError}</p> : null}
      {events.length === 0 ? <p className="mt-2 text-sm text-zinc-400">No bot audit events yet.</p> : null}
      <div className="mt-2 grid gap-2">
        {events.map((event) => (
          <article key={event.id} className="border border-zinc-700 bg-black/40 p-2 text-xs">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <strong className="text-cyan-100">{event.trigger || event.command || event.action || 'bot action'}</strong>
              <span className="hud-chip hud-chip--square">{event.result}</span>
            </div>
            <p className="mt-1 text-zinc-300">{event.actorDisplayName || event.actorLogin || event.source} · {event.reason || 'no reason'}</p>
            <p className="mt-1 text-zinc-500">{event.createdAt}</p>
          </article>
        ))}
      </div>
    </section>
  );
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
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Bot commands</p>
      <p className="mt-1 text-sm text-zinc-300">Create safe Twitch chat responses. Custom commands can publish ticker/alert overlays, send guarded Twitch chat replies, or both. They do not run scripts, call arbitrary APIs, or trigger OBS actions.</p>
      {commands?.loadError ? <p className="mt-2 border border-amber-400/40 bg-black px-2 py-1 text-xs text-amber-100">commands unavailable :: {commands.loadError}</p> : null}

      <div className="mt-3 grid gap-2 md:grid-cols-[1fr_8rem_8rem_10rem_8rem]">
        <label className="grid gap-1 text-xs text-zinc-300">
          Command name
          <input className="hud-input" value={draft.name} onChange={(event) => onDraftChange({ ...draft, name: event.target.value })} placeholder="discord" />
        </label>
        <label className="grid gap-1 text-xs text-zinc-300">
          Response mode
          <select className="hud-input" value={draft.responseMode} onChange={(event) => onDraftChange({ ...draft, responseMode: event.target.value as CommandDraft['responseMode'] })}>
            <option value="overlay">Overlay</option>
            <option value="chat">Chat</option>
            <option value="both">Both</option>
          </select>
        </label>
        <label className="grid gap-1 text-xs text-zinc-300">
          Action
          <select className="hud-input" value={draft.action} onChange={(event) => onDraftChange({ ...draft, action: event.target.value as CommandDraft['action'] })}>
            <option value="ticker">Ticker</option>
            <option value="alert">Alert</option>
          </select>
        </label>
        <label className="grid gap-1 text-xs text-zinc-300">
          Required role
          <select className="hud-input" value={draft.roleRequired} onChange={(event) => onDraftChange({ ...draft, roleRequired: event.target.value as CommandDraft['roleRequired'] })}>
            <option value="viewer">Viewer</option>
            <option value="moderator">Moderator</option>
            <option value="broadcaster">Broadcaster</option>
          </select>
        </label>
        <label className="grid gap-1 text-xs text-zinc-300">
          Cooldown
          <input className="hud-input" type="number" min={0} value={draft.cooldownSeconds} onChange={(event) => onDraftChange({ ...draft, cooldownSeconds: Number(event.target.value) })} />
        </label>
      </div>
      <label className="mt-2 grid gap-1 text-xs text-zinc-300">
        Response
        <textarea className="hud-input min-h-20" value={draft.response} onChange={(event) => onDraftChange({ ...draft, response: event.target.value })} placeholder="Join the Discord: https://example.test" />
      </label>
      <div className="mt-2 border border-cyan-400/20 bg-black/40 p-2 text-xs">
        <p className="text-zinc-500">Preview with sample context</p>
        <p className="mt-1 text-cyan-100">{renderCommandPreview(draft) || 'Enter a response to preview rendered output.'}</p>
      </div>
      <div className="mt-2 flex items-center gap-3">
        <label className="flex items-center gap-2 text-xs text-zinc-300">
          <input type="checkbox" checked={draft.enabled} onChange={(event) => onDraftChange({ ...draft, enabled: event.target.checked })} />
          enabled
        </label>
        <button className="hud-button" type="button" disabled={!canSave || saving} onClick={onSave}>{saving ? 'Saving…' : 'Save command'}</button>
      </div>

      {items.length === 0 ? <p className="mt-3 text-sm text-zinc-400">No bot commands configured yet.</p> : null}
      <div className="mt-3 grid gap-2">
        {items.map((command) => (
          <article key={command.id} className="border border-zinc-700 bg-black/40 p-2 text-xs">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <strong className="text-cyan-100">!{command.name}</strong>
              <span className="hud-chip hud-chip--square">{command.enabled ? 'enabled' : 'paused'}</span>
            </div>
            <p className="mt-1 text-zinc-300">{command.action} · {command.responseMode || 'overlay'} · {command.roleRequired}+ · {command.cooldownSeconds}s cooldown</p>
            <p className="mt-1 text-zinc-400">{command.response}</p>
            <div className="mt-2 flex gap-2">
              <button className="hud-button hud-button--small" type="button" onClick={() => onToggle(command)}>{command.enabled ? 'Pause' : 'Enable'}</button>
              <button className="hud-button hud-button--small" type="button" onClick={() => onDelete(command)}>Delete</button>
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}

function renderCommandPreview(draft: CommandDraft) {
  const values: Record<string, string> = {
    user: 'TestViewer',
    displayName: 'TestViewer',
    channel: 'patrick__eff',
    args: 'sample args',
    command: draft.name.trim().replace(/^!/, '') || 'command',
    'nowPlaying.title': '',
    'nowPlaying.artist': '',
    'quote:random': 'Example quote — ExampleAuthor',
  };
  return draft.response.replace(/\{([^{}]+)\}/g, (_match, key: string) => {
    if (key.startsWith('count:')) return '0';
    if (key.startsWith('api:')) return 'API preview';
    return values[key] ?? '';
  }).trim().slice(0, 400);
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
  counterDraft: { name: string; value: number };
  quoteDraft: { text: string; author: string };
  onCounterDraftChange: (draft: { name: string; value: number }) => void;
  onQuoteDraftChange: (draft: { text: string; author: string }) => void;
  onSaveCounter: () => void;
  onDeleteCounter: (counter: BotCounter) => void;
  onSaveQuote: () => void;
  onDeleteQuote: (quote: BotQuote) => void;
}) {
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Bot counters and quotes</p>
      <p className="mt-1 text-sm text-zinc-300">Community tools for `!count name`, `!quote`, and moderator `!quote add text`.</p>
      <div className="mt-3 grid gap-3 lg:grid-cols-2">
        <div>
          <p className="text-xs uppercase tracking-wide text-zinc-500">Counters</p>
          <div className="mt-2 grid gap-2 md:grid-cols-[1fr_7rem_auto]">
            <input className="hud-input" value={counterDraft.name} onChange={(event) => onCounterDraftChange({ ...counterDraft, name: event.target.value })} placeholder="hype" />
            <input className="hud-input" type="number" value={counterDraft.value} onChange={(event) => onCounterDraftChange({ ...counterDraft, value: Number(event.target.value) })} />
            <button className="hud-button" type="button" disabled={counterDraft.name.trim() === ''} onClick={onSaveCounter}>Save counter</button>
          </div>
          {(counters?.counters ?? []).length === 0 ? <p className="mt-2 text-sm text-zinc-400">No counters yet.</p> : null}
          <div className="mt-2 grid gap-2">
            {(counters?.counters ?? []).map((counter) => <article key={counter.id} className="flex items-center justify-between border border-zinc-700 bg-black/40 p-2 text-xs"><span><strong className="text-cyan-100">{counter.name}</strong> · {counter.value}</span><button className="hud-button hud-button--small" type="button" onClick={() => onDeleteCounter(counter)}>Delete</button></article>)}
          </div>
        </div>
        <div>
          <p className="text-xs uppercase tracking-wide text-zinc-500">Quotes</p>
          <input className="hud-input mt-2" value={quoteDraft.author} onChange={(event) => onQuoteDraftChange({ ...quoteDraft, author: event.target.value })} placeholder="author optional" />
          <textarea className="hud-input mt-2 min-h-20" value={quoteDraft.text} onChange={(event) => onQuoteDraftChange({ ...quoteDraft, text: event.target.value })} placeholder="quote text" />
          <button className="hud-button mt-2" type="button" disabled={quoteDraft.text.trim() === ''} onClick={onSaveQuote}>Save quote</button>
          {(quotes?.quotes ?? []).length === 0 ? <p className="mt-2 text-sm text-zinc-400">No quotes yet.</p> : null}
          <div className="mt-2 grid gap-2">
            {(quotes?.quotes ?? []).map((quote) => <article key={quote.id} className="border border-zinc-700 bg-black/40 p-2 text-xs"><p className="text-zinc-300">{quote.text}</p><div className="mt-1 flex items-center justify-between"><span className="text-zinc-500">{quote.author || 'unknown'}</span><button className="hud-button hud-button--small" type="button" onClick={() => onDeleteQuote(quote)}>Delete</button></div></article>)}
          </div>
        </div>
      </div>
    </section>
  );
}

function BotAPIVariablesPanel({ variables, draft, onDraftChange, onSave, onDelete }: { variables: BotAPIVariablesResponse | null; draft: typeof defaultAPIVariableDraft; onDraftChange: (draft: typeof defaultAPIVariableDraft) => void; onSave: () => void; onDelete: (variable: BotAPIVariable) => void }) {
  const items = variables?.variables ?? [];
  const canSave = draft.name.trim() !== '' && draft.url.trim().startsWith('https://');
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Bot API variables</p>
      <p className="mt-1 text-sm text-zinc-300">Operator-defined read-only HTTP variables for templates like <code>{'{api:weather}'}</code>. GET-only, HTTPS-only, capped, cached, and blocked from private hosts.</p>
      {variables?.loadError ? <p className="mt-2 border border-amber-400/40 bg-black px-2 py-1 text-xs text-amber-100">api variables unavailable :: {variables.loadError}</p> : null}
      <div className="mt-3 grid gap-2 md:grid-cols-[1fr_2fr_10rem_8rem]">
        <label className="grid gap-1 text-xs text-zinc-300">Name<input className="hud-input" value={draft.name} onChange={(event) => onDraftChange({ ...draft, name: event.target.value })} placeholder="weather" /></label>
        <label className="grid gap-1 text-xs text-zinc-300">HTTPS URL<input className="hud-input" value={draft.url} onChange={(event) => onDraftChange({ ...draft, url: event.target.value })} placeholder="https://api.example.test/value" /></label>
        <label className="grid gap-1 text-xs text-zinc-300">JSON selector<input className="hud-input" value={draft.responseSelector} onChange={(event) => onDraftChange({ ...draft, responseSelector: event.target.value })} placeholder="data.value" /></label>
        <label className="grid gap-1 text-xs text-zinc-300">TTL seconds<input className="hud-input" type="number" min={15} max={3600} value={draft.cacheTtlSeconds} onChange={(event) => onDraftChange({ ...draft, cacheTtlSeconds: Number(event.target.value) })} /></label>
      </div>
      <div className="mt-2 grid gap-2 md:grid-cols-[1fr_8rem]">
        <label className="grid gap-1 text-xs text-zinc-300">Fallback<input className="hud-input" value={draft.fallback} onChange={(event) => onDraftChange({ ...draft, fallback: event.target.value })} placeholder="unavailable" /></label>
        <label className="grid gap-1 text-xs text-zinc-300">Timeout ms<input className="hud-input" type="number" min={250} max={3000} value={draft.timeoutMs} onChange={(event) => onDraftChange({ ...draft, timeoutMs: Number(event.target.value) })} /></label>
      </div>
      <div className="mt-2 flex items-center gap-3">
        <label className="flex items-center gap-2 text-xs text-zinc-300"><input type="checkbox" checked={draft.enabled} onChange={(event) => onDraftChange({ ...draft, enabled: event.target.checked })} />enabled</label>
        <button className="hud-button" type="button" disabled={!canSave} onClick={onSave}>Save API variable</button>
      </div>
      {items.length === 0 ? <p className="mt-3 text-sm text-zinc-400">No API variables configured yet.</p> : null}
      <div className="mt-3 grid gap-2">
        {items.map((variable) => (
          <article key={variable.id} className="border border-zinc-700 bg-black/40 p-2 text-xs">
            <div className="flex flex-wrap items-center justify-between gap-2"><strong className="text-cyan-100">{`{api:${variable.name}}`}</strong><span className="hud-chip hud-chip--square">{variable.enabled ? 'enabled' : 'paused'}</span></div>
            <p className="mt-1 break-all text-zinc-400">{variable.url}</p>
            <p className="mt-1 text-zinc-500">status {variable.lastStatus || 'untested'} · ttl {variable.cacheTtlSeconds}s · timeout {variable.timeoutMs}ms</p>
            {variable.lastError ? <p className="mt-1 text-amber-200">{variable.lastError}</p> : null}
            <button className="hud-button hud-button--small mt-2" type="button" onClick={() => onDelete(variable)}>Delete</button>
          </article>
        ))}
      </div>
    </section>
  );
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
  onDraftChange,
  onTestChange,
  onActionChange,
  onNukeDraftChange,
  onSave,
  onDelete,
  onDryRun,
  onAction,
  onNukeDryRun,
}: {
  rules: BotModerationRulesResponse | null;
  draft: ModerationDraft;
  test: { username: string; message: string };
  dryRun: BotModerationDryRunResponse | null;
  actionDraft: typeof defaultModerationAction;
  actionResult: string;
  nukeDraft: typeof defaultNukeDraft;
  nukeResult: BotNukeDryRunResponse | null;
  onDraftChange: (draft: ModerationDraft) => void;
  onTestChange: (test: { username: string; message: string }) => void;
  onActionChange: (draft: typeof defaultModerationAction) => void;
  onNukeDraftChange: (draft: typeof defaultNukeDraft) => void;
  onSave: () => void;
  onDelete: (rule: BotModerationRule) => void;
  onDryRun: () => void;
  onAction: () => void;
  onNukeDryRun: () => void;
}) {
  const items = rules?.rules ?? [];
  const canSave = draft.name.trim() !== '' && draft.pattern.trim() !== '';
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Bot moderation dry-run</p>
      <p className="mt-1 text-sm text-zinc-300">Blocked-term rules are stored and testable here. Actions are audit/planning labels only for now; no Twitch delete, timeout, or ban calls are sent.</p>
      {rules?.loadError ? <p className="mt-2 border border-amber-400/40 bg-black px-2 py-1 text-xs text-amber-100">moderation unavailable :: {rules.loadError}</p> : null}
      <div className="mt-3 grid gap-2 md:grid-cols-[1fr_1fr_9rem_8rem]">
        <input className="hud-input" value={draft.name} onChange={(event) => onDraftChange({ ...draft, name: event.target.value })} placeholder="spam_link" />
        <input className="hud-input" value={draft.pattern} onChange={(event) => onDraftChange({ ...draft, pattern: event.target.value })} placeholder="buy now" />
        <select className="hud-input" value={draft.matchTarget} onChange={(event) => onDraftChange({ ...draft, matchTarget: event.target.value as ModerationDraft['matchTarget'] })}>
          <option value="message">Message</option>
          <option value="username">Username</option>
          <option value="both">Both</option>
        </select>
        <select className="hud-input" value={draft.action} onChange={(event) => onDraftChange({ ...draft, action: event.target.value as ModerationDraft['action'] })}>
          <option value="flag">Flag</option>
          <option value="delete">Delete label</option>
          <option value="timeout">Timeout label</option>
          <option value="ban">Ban label</option>
        </select>
      </div>
      <input className="hud-input mt-2" value={draft.reason} onChange={(event) => onDraftChange({ ...draft, reason: event.target.value })} placeholder="reason shown in audit/planning" />
      <div className="mt-2 flex flex-wrap items-center gap-3 text-xs text-zinc-300">
        <label className="flex items-center gap-2"><input type="checkbox" checked={draft.enabled} onChange={(event) => onDraftChange({ ...draft, enabled: event.target.checked })} /> enabled</label>
        <label className="flex items-center gap-2"><input type="checkbox" checked={draft.regex} onChange={(event) => onDraftChange({ ...draft, regex: event.target.checked })} /> regex</label>
        <label className="flex items-center gap-2"><input type="checkbox" checked={draft.caseSensitive} onChange={(event) => onDraftChange({ ...draft, caseSensitive: event.target.checked })} /> case sensitive</label>
        <button className="hud-button" type="button" disabled={!canSave} onClick={onSave}>Save rule</button>
      </div>
      <div className="mt-4 grid gap-2 md:grid-cols-[10rem_1fr_auto]">
        <input className="hud-input" value={test.username} onChange={(event) => onTestChange({ ...test, username: event.target.value })} placeholder="username" />
        <input className="hud-input" value={test.message} onChange={(event) => onTestChange({ ...test, message: event.target.value })} placeholder="message to test" />
        <button className="hud-button" type="button" onClick={onDryRun}>Dry run</button>
      </div>
      {dryRun ? <p className="mt-2 text-xs text-zinc-300">matches :: {dryRun.matches.length ? dryRun.matches.map((match) => `${match.ruleName}:${match.action}`).join(', ') : 'none'}</p> : null}

      <div className="mt-4 border border-amber-400/30 bg-black/40 p-2">
        <p className="text-xs uppercase tracking-wide text-amber-100">Manual Twitch moderation action</p>
        <p className="mt-1 text-xs text-zinc-400">Requires Twitch moderation scopes. Use only after dry-run evidence; failed/successful attempts are written to bot audit.</p>
        <div className="mt-2 grid gap-2 md:grid-cols-[9rem_1fr_1fr_8rem]">
          <select className="hud-input" value={actionDraft.action} onChange={(event) => onActionChange({ ...actionDraft, action: event.target.value as typeof defaultModerationAction.action })}>
            <option value="delete">Delete message</option>
            <option value="timeout">Timeout user</option>
            <option value="ban">Ban user</option>
            <option value="unban">Unban user</option>
          </select>
          <input className="hud-input" value={actionDraft.targetUserId} onChange={(event) => onActionChange({ ...actionDraft, targetUserId: event.target.value })} placeholder="target user id" />
          <input className="hud-input" value={actionDraft.messageId} onChange={(event) => onActionChange({ ...actionDraft, messageId: event.target.value })} placeholder="message id for delete" />
          <input className="hud-input" type="number" min={1} value={actionDraft.durationSeconds} onChange={(event) => onActionChange({ ...actionDraft, durationSeconds: Number(event.target.value) })} />
        </div>
        <input className="hud-input mt-2" value={actionDraft.reason} onChange={(event) => onActionChange({ ...actionDraft, reason: event.target.value })} placeholder="reason" />
        <div className="mt-2 flex items-center gap-2">
          <button className="hud-button" type="button" onClick={onAction}>Run guarded action</button>
          {actionResult ? <span className="text-xs text-emerald-200">{actionResult}</span> : null}
        </div>
      </div>

      <div className="mt-4 border border-cyan-400/30 bg-black/40 p-2">
        <p className="text-xs uppercase tracking-wide text-cyan-100">Nuke dry-run planner</p>
        <p className="mt-1 text-xs text-zinc-400">Scans recent persisted Twitch chat history and reports matching messages. This does not delete messages or timeout users.</p>
        <div className="mt-2 grid gap-2 md:grid-cols-[1fr_8rem_8rem_auto]">
          <input className="hud-input" value={nukeDraft.pattern} onChange={(event) => onNukeDraftChange({ ...nukeDraft, pattern: event.target.value })} placeholder="phrase or regex" />
          <input className="hud-input" type="number" min={1} max={360} value={nukeDraft.lookbackMinutes} onChange={(event) => onNukeDraftChange({ ...nukeDraft, lookbackMinutes: Number(event.target.value) })} />
          <input className="hud-input" type="number" min={1} max={250} value={nukeDraft.limit} onChange={(event) => onNukeDraftChange({ ...nukeDraft, limit: Number(event.target.value) })} />
          <button className="hud-button" type="button" disabled={nukeDraft.pattern.trim() === ''} onClick={onNukeDryRun}>Plan nuke</button>
        </div>
        <div className="mt-2 flex flex-wrap gap-3 text-xs text-zinc-300">
          <label className="flex items-center gap-2"><input type="checkbox" checked={nukeDraft.regex} onChange={(event) => onNukeDraftChange({ ...nukeDraft, regex: event.target.checked })} /> regex</label>
          <label className="flex items-center gap-2"><input type="checkbox" checked={nukeDraft.caseSensitive} onChange={(event) => onNukeDraftChange({ ...nukeDraft, caseSensitive: event.target.checked })} /> case sensitive</label>
        </div>
        {nukeResult ? <p className="mt-2 text-xs text-zinc-300">planned matches :: {nukeResult.matches.length}</p> : null}
        <div className="mt-2 grid gap-1">
          {(nukeResult?.matches ?? []).slice(0, 5).map((match) => <div key={match.eventId} className="hud-terminal-line"><span className="hud-terminal-key">{match.user}</span><span className="hud-terminal-value">{match.message}</span></div>)}
        </div>
      </div>

      {items.length === 0 ? <p className="mt-3 text-sm text-zinc-400">No moderation rules configured yet.</p> : null}
      <div className="mt-3 grid gap-2">
        {items.map((rule) => <article key={rule.id} className="border border-zinc-700 bg-black/40 p-2 text-xs"><div className="flex items-center justify-between gap-2"><strong className="text-cyan-100">{rule.name}</strong><span className="hud-chip hud-chip--square">{rule.enabled ? 'enabled' : 'paused'}</span></div><p className="mt-1 text-zinc-300">{rule.matchTarget} · {rule.regex ? 'regex' : 'contains'} · {rule.action}</p><p className="mt-1 text-zinc-400">{rule.pattern}</p><div className="mt-2 flex justify-between gap-2"><span className="text-zinc-500">{rule.reason || 'no reason'}</span><button className="hud-button hud-button--small" type="button" onClick={() => onDelete(rule)}>Delete</button></div></article>)}
      </div>
    </section>
  );
}

function BotTimersPanel({
  draft,
  timers,
  saving,
  onDraftChange,
  onSave,
  onToggle,
  onDelete,
}: {
  draft: TimerDraft;
  timers: BotTimersResponse | null;
  saving: boolean;
  onDraftChange: (draft: TimerDraft) => void;
  onSave: () => void;
  onToggle: (timer: BotTimer) => void;
  onDelete: (timer: BotTimer) => void;
}) {
  const items = timers?.timers ?? [];
  const canSave = draft.name.trim() !== '' && draft.response.trim() !== '' && draft.intervalSeconds >= 30;
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Bot timers</p>
      <p className="mt-1 text-sm text-zinc-300">Create safe scheduled bot messages now; runtime firing is enabled after response-command/template support lands.</p>
      {timers?.loadError ? <p className="mt-2 border border-amber-400/40 bg-black px-2 py-1 text-xs text-amber-100">timers unavailable :: {timers.loadError}</p> : null}

      <div className="mt-3 grid gap-2 md:grid-cols-[1fr_9rem_8rem]">
        <label className="grid gap-1 text-xs text-zinc-300">
          Timer name
          <input className="hud-input" value={draft.name} onChange={(event) => onDraftChange({ ...draft, name: event.target.value })} placeholder="stream_hype" />
        </label>
        <label className="grid gap-1 text-xs text-zinc-300">
          Interval seconds
          <input className="hud-input" type="number" min={30} value={draft.intervalSeconds} onChange={(event) => onDraftChange({ ...draft, intervalSeconds: Number(event.target.value) })} />
        </label>
        <label className="grid gap-1 text-xs text-zinc-300">
          Action
          <select className="hud-input" value={draft.action} onChange={(event) => onDraftChange({ ...draft, action: event.target.value as TimerDraft['action'] })}>
            <option value="ticker">Ticker</option>
            <option value="alert">Alert</option>
          </select>
        </label>
      </div>
      <label className="mt-2 grid gap-1 text-xs text-zinc-300">
        Response
        <textarea className="hud-input min-h-20" value={draft.response} onChange={(event) => onDraftChange({ ...draft, response: event.target.value })} placeholder="Hydrate, stretch, and check posture." />
      </label>
      <div className="mt-2 flex items-center gap-3">
        <label className="flex items-center gap-2 text-xs text-zinc-300">
          <input type="checkbox" checked={draft.enabled} onChange={(event) => onDraftChange({ ...draft, enabled: event.target.checked })} />
          enabled
        </label>
        <button className="hud-button" type="button" disabled={!canSave || saving} onClick={onSave}>{saving ? 'Saving…' : 'Save timer'}</button>
      </div>

      {items.length === 0 ? <p className="mt-3 text-sm text-zinc-400">No bot timers configured yet.</p> : null}
      <div className="mt-3 grid gap-2">
        {items.map((timer) => (
          <article key={timer.id} className="border border-zinc-700 bg-black/40 p-2 text-xs">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <strong className="text-cyan-100">{timer.name}</strong>
              <span className="hud-chip hud-chip--square">{timer.enabled ? 'enabled' : 'paused'}</span>
            </div>
            <p className="mt-1 text-zinc-300">every {timer.intervalSeconds}s · {timer.action}</p>
            <p className="mt-1 text-zinc-400">{timer.response}</p>
            <div className="mt-2 flex gap-2">
              <button className="hud-button hud-button--small" type="button" onClick={() => onToggle(timer)}>{timer.enabled ? 'Pause' : 'Enable'}</button>
              <button className="hud-button hud-button--small" type="button" onClick={() => onDelete(timer)}>Delete</button>
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}

function Line({ label, value }: { label: string; value: string }) {
  return <div className="hud-terminal-line"><span className="hud-terminal-key">{label}</span><span className="hud-terminal-value">{value}</span></div>;
}
