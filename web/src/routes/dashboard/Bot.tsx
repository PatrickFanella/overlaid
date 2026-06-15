import { useEffect, useState } from 'react';
import {
  deleteBotCommand,
  deleteBotTimer,
  fetchBotAudit,
  fetchBotCommands,
  fetchBotStatus,
  fetchBotTimers,
  saveBotCommand,
  saveBotTimer,
  setBotCommandEnabled,
  setBotTimerEnabled,
  type BotAuditResponse,
  type BotCommand,
  type BotCommandsResponse,
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
  timers: BotTimersResponse | null;
  loading: boolean;
  error: string;
};

type CommandDraft = {
  name: string;
  response: string;
  action: 'ticker' | 'alert';
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

const defaultTimerDraft: TimerDraft = { name: '', intervalSeconds: 300, response: '', action: 'ticker', enabled: true };
const defaultCommandDraft: CommandDraft = { name: '', response: '', action: 'ticker', roleRequired: 'viewer', cooldownSeconds: 10, enabled: true };

export function DashboardBotPage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const operatorSecret = loadOperatorSecret();
  const [state, setState] = useState<BotPageState>({ status: null, audit: null, commands: null, timers: null, loading: false, error: '' });
  const [commandDraft, setCommandDraft] = useState<CommandDraft>(defaultCommandDraft);
  const [draft, setDraft] = useState<TimerDraft>(defaultTimerDraft);
  const [savingCommand, setSavingCommand] = useState(false);
  const [savingTimer, setSavingTimer] = useState(false);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      setState((current) => ({ ...current, loading: true, error: '' }));
      try {
        const [status, audit, commands, timers] = await Promise.all([fetchBotStatus(apiBaseUrl, operatorSecret), fetchBotAudit(25, apiBaseUrl, operatorSecret), fetchBotCommands(apiBaseUrl, operatorSecret), fetchBotTimers(apiBaseUrl, operatorSecret)]);
        if (!cancelled) {
          setState({ status, audit, commands, timers, loading: false, error: '' });
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
      <p className="mt-1 text-sm text-zinc-300">Create safe Twitch chat responses. Custom commands can only publish ticker or alert overlays; they do not run scripts, call APIs, or send outbound chat.</p>
      {commands?.loadError ? <p className="mt-2 border border-amber-400/40 bg-black px-2 py-1 text-xs text-amber-100">commands unavailable :: {commands.loadError}</p> : null}

      <div className="mt-3 grid gap-2 md:grid-cols-[1fr_8rem_10rem_8rem]">
        <label className="grid gap-1 text-xs text-zinc-300">
          Command name
          <input className="hud-input" value={draft.name} onChange={(event) => onDraftChange({ ...draft, name: event.target.value })} placeholder="discord" />
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
            <p className="mt-1 text-zinc-300">{command.action} · {command.roleRequired}+ · {command.cooldownSeconds}s cooldown</p>
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
