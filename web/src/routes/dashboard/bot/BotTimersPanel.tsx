import type { BotTimer, BotTimersResponse } from '../../../lib/api';
import type { TimerDraft } from './types';
import { Check, Empty, Field, Notice, Panel, Tag } from '../ui';

export function BotTimersPanel({
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
                <Tag tone={timer.enabled ? 'ok' : 'neutral'}>{timer.enabled ? 'enabled' : 'paused'}</Tag>
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
