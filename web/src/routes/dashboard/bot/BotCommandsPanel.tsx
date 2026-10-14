import type { BotCommandsState } from './useBotCommands';
import type { CommandDraft } from './types';
import { renderCommandPreview } from './renderCommandPreview';
import { Check, Empty, Field, Notice, Panel, SubPanel, Tag } from '../ui';

export function BotCommandsPanel({ draft, commands, loading, pending, error, result, onDraftChange, onSave, onToggle, onDelete, onEdit, onCancel }: BotCommandsState) {
  const items = commands?.commands ?? [];
  const busy = loading || Boolean(pending);
  const name = draft.name.trim().replace(/^!/, '').toLowerCase();
  const newsroom = draft.action.startsWith('newsroom_');
  const canSave = /^[a-z0-9][a-z0-9_-]{0,31}$/.test(name) && (newsroom || draft.response.trim() !== '') && new TextEncoder().encode(draft.response.trim()).length <= 500 && Number.isInteger(draft.cooldownSeconds) && draft.cooldownSeconds >= 0 && draft.cooldownSeconds <= 3600;
  return (
    <Panel
      id="bot-commands"
      kicker="Chat commands"
      title="Bot commands"
      description="Create safe Twitch chat responses. Commands can publish overlays, send Twitch replies, submit Newsroom links/questions, report the current source, or enter a raffle. Names, enabled states, roles and cooldowns are managed here. They do not run scripts, call arbitrary APIs, or trigger OBS actions."
    >
      {loading ? <p className="desk-note">Loading commands…</p> : null}
      {error ? <Notice tone="bad" role="alert">{error}</Notice> : null}
      {result ? <Notice tone="ok" role="status">{result}</Notice> : null}
      {draft.id ? <p className="desk-note">Editing command {draft.id}.</p> : null}
      {commands?.loadError ? <Notice tone="warn">Commands unavailable: {commands.loadError}</Notice> : null}

      <div className="grid gap-3 md:grid-cols-[1fr_8rem_8rem_10rem_8rem]">
        <Field label="Command name">
          <input className="desk-input desk-input--mono" disabled={busy} value={draft.name} onChange={(event) => onDraftChange({ ...draft, name: event.target.value })} placeholder="discord" />
        </Field>
        <Field label="Response mode">
          <select className="desk-input" disabled={busy || newsroom} value={draft.responseMode} onChange={(event) => onDraftChange({ ...draft, responseMode: event.target.value as CommandDraft['responseMode'] })}>
            <option value="overlay">Overlay</option>
            <option value="chat">Chat</option>
            <option value="both">Both</option>
          </select>
        </Field>
        <Field label="Action">
          <select className="desk-input" disabled={busy} value={draft.action} onChange={(event) => onDraftChange({ ...draft, action: event.target.value as CommandDraft['action'], ...(event.target.value.startsWith('newsroom_') ? {response:'',responseMode:'chat'} : {}) })}>
            <option value="ticker">Ticker</option>
            <option value="alert">Alert</option>
            <option value="newsroom_submit">Newsroom: submit link</option><option value="newsroom_capture">Newsroom: capture bare links</option><option value="newsroom_current">Newsroom: current source</option><option value="newsroom_question">Newsroom: submit question</option><option value="newsroom_raffle">Newsroom: enter raffle</option>
          </select>
        </Field>
        <Field label="Required role">
          <select className="desk-input" disabled={busy} value={draft.roleRequired} onChange={(event) => onDraftChange({ ...draft, roleRequired: event.target.value as CommandDraft['roleRequired'] })}>
            <option value="viewer">Viewer</option><option value="supporter">Subscriber or VIP</option>
            <option value="moderator">Moderator</option>
            <option value="broadcaster">Broadcaster</option>
            {draft.roleRequired === 'operator' || draft.roleRequired === 'system' ? <option value={draft.roleRequired}>{draft.roleRequired}</option> : null}
          </select>
        </Field>
        <Field label="Cooldown seconds">
          <input className="desk-input" type="number" min={0} max={3600} step={1} disabled={busy} value={draft.cooldownSeconds} onChange={(event) => onDraftChange({ ...draft, cooldownSeconds: Number(event.target.value) })} />
        </Field>
      </div>
      {newsroom ? <Notice>Newsroom actions use the submitted arguments and generate their own result. <a href="/dashboard/newsroom">Open the reaction desk</a> to manage material and raffles.</Notice> : <><Field label="Response">
        <textarea className="desk-input" disabled={busy} value={draft.response} onChange={(event) => onDraftChange({ ...draft, response: event.target.value })} placeholder="Join the Discord: https://example.test" />
      </Field>
      <SubPanel title="Preview with sample context">
        <p className="mono">{renderCommandPreview(draft) || 'Enter a response to preview rendered output.'}</p>
      </SubPanel></>}
      <div className="flex flex-wrap items-center gap-3">
        <fieldset disabled={busy}><Check label="Enabled" checked={draft.enabled} onChange={(enabled) => onDraftChange({ ...draft, enabled })} /></fieldset>
        <button className="desk-btn desk-btn--primary" type="button" disabled={!canSave || busy} onClick={onSave}>
          {pending?.action === 'save' ? 'Saving…' : draft.id ? 'Save changes' : 'Save command'}
        </button>
        {draft.id ? <button className="desk-btn" type="button" disabled={busy} onClick={onCancel}>Cancel edit</button> : null}
      </div>

      {!loading && !commands?.loadError && items.length === 0 ? (
        <Empty>No bot commands configured yet.</Empty>
      ) : (
        <ul className="desk-rows">
          {items.map((command) => (
            <li key={command.id} className="desk-row">
              <div className="desk-row__line">
                <p className="desk-row__title mono">!{command.name}</p>
                <Tag tone={command.enabled ? 'ok' : 'neutral'}>{command.enabled ? 'enabled' : 'paused'}</Tag>
              </div>
              <p className="desk-row__meta">
                {command.action} · {command.responseMode || 'overlay'} · {command.roleRequired}+ · {command.cooldownSeconds}s cooldown
              </p>
              <p className="desk-row__text">{command.response}</p>
              <div className="flex flex-wrap gap-2 pt-1">
                <button className="desk-btn desk-btn--small" type="button" disabled={busy} onClick={() => onEdit(command)}>Edit</button>
                <button className="desk-btn desk-btn--small" type="button" disabled={busy} onClick={() => onToggle(command)}>
                  {command.enabled ? 'Pause' : 'Enable'}
                </button>
                <button className="desk-btn desk-btn--small desk-btn--danger" type="button" disabled={busy} onClick={() => onDelete(command)}>
                  {pending?.action === 'delete' && pending.id === command.id ? 'Deleting…' : 'Delete'}
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

