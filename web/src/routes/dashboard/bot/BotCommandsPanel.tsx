import type { BotCommand, BotCommandsResponse } from '../../../lib/api';
import type { CommandDraft } from './types';
import { renderCommandPreview } from './renderCommandPreview';
import { Check, Empty, Field, Notice, Panel, SubPanel, Tag } from '../ui';

export function BotCommandsPanel({
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
                <Tag tone={command.enabled ? 'ok' : 'neutral'}>{command.enabled ? 'enabled' : 'paused'}</Tag>
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

