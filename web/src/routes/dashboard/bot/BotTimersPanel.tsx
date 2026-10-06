import type { BotTimersState } from './useBotTimers';
import type { TimerDraft } from './types';
import { Check, Empty, Field, Notice, Panel, Tag } from '../ui';

export function BotTimersPanel({
  draft, timers, pending, loading, error, result,
  onRun, onEdit, onCancel, onDraftChange, onSave, onToggle, onDelete,
}: BotTimersState) {
  const items = timers?.timers ?? [];
  const responseTooLong = new TextEncoder().encode(draft.response.trim()).length > 500;
  const canSave = /^[a-z0-9_-]+$/.test(draft.name.trim().toLowerCase()) && draft.response.trim() !== '' && !responseTooLong && Number.isInteger(draft.intervalSeconds) && draft.intervalSeconds >= 30 && draft.intervalSeconds <= 86400;
  const busy = pending !== null || loading;
  return (
    <Panel
      id="bot-timers"
      kicker="Schedule"
      title="Bot timers"
      description="Enabled timers send ticker or alert events when due. Run once rehearses any saved timer without changing its schedule."
    >
      {timers?.loadError ? <Notice tone="warn">Timers unavailable: {timers.loadError}</Notice> : null}

      {error ? <Notice tone="bad" role="alert">{error}</Notice> : null}
      {draft.id ? <Notice>Editing {draft.name || 'saved timer'}. Save changes updates this timer.</Notice> : null}

      <fieldset className="grid min-w-0 gap-3" disabled={busy}>
        <legend className="sr-only">Timer editor</legend>
        <div className="grid gap-3 md:grid-cols-[1fr_9rem_8rem]">
          <Field label="Timer name" hint="Letters, numbers, dash, or underscore.">
            <input className="desk-input desk-input--mono" value={draft.name} onChange={(event) => onDraftChange({ ...draft, name: event.target.value })} placeholder="stream_hype" />
          </Field>
          <Field label="Interval seconds" hint="30 to 86,400 seconds.">
            <input className="desk-input" type="number" min={30} max={86400} step={1} value={draft.intervalSeconds} onChange={(event) => onDraftChange({ ...draft, intervalSeconds: Number(event.target.value) })} />
          </Field>
          <Field label="Action">
            <select className="desk-input" value={draft.action} onChange={(event) => onDraftChange({ ...draft, action: event.target.value as TimerDraft['action'] })}>
              <option value="ticker">Ticker</option>
              <option value="alert">Alert</option>
            </select>
          </Field>
        </div>
        <Field label="Response" hint={responseTooLong ? 'Response is too long. Shorten it before saving.' : undefined}>
          <textarea className="desk-input" maxLength={500} value={draft.response} onChange={(event) => onDraftChange({ ...draft, response: event.target.value })} placeholder="Hydrate, stretch, and check posture." />
        </Field>
        <div className="flex flex-wrap items-center gap-3">
          <Check label="Enabled" checked={draft.enabled} onChange={(enabled) => onDraftChange({ ...draft, enabled })} />
          <button className="desk-btn desk-btn--primary" type="button" disabled={!canSave || busy} onClick={onSave}>
            {pending?.action === 'save' ? 'Saving…' : draft.id ? 'Save changes' : 'Save timer'}
          </button>
          {draft.id ? <button className="desk-btn" type="button" onClick={onCancel}>Cancel edit</button> : null}
        </div>
      </fieldset>

      {result ? <Notice tone="ok" role="status">{result}</Notice> : null}

      {loading ? <Empty>Loading timers…</Empty> : items.length === 0 ? (
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
                <button className="desk-btn desk-btn--small" type="button" disabled={busy} onClick={() => onRun(timer)}>
                  {pending?.action === 'run' && pending.id === timer.id ? 'Running…' : 'Run once'}
                </button>
                <button className="desk-btn desk-btn--small" type="button" disabled={busy} onClick={() => onEdit(timer)}>
                  Edit
                </button>
                <button className="desk-btn desk-btn--small" type="button" disabled={busy} onClick={() => onToggle(timer)}>
                  {timer.enabled ? 'Pause' : 'Enable'}
                </button>
                <button className="desk-btn desk-btn--small desk-btn--danger" type="button" disabled={busy} onClick={() => onDelete(timer)}>
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
