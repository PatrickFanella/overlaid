import type { BotAPIVariable, BotAPIVariablesResponse } from '../../../lib/api';
import type { APIVariableDraft } from './types';
import { Check, Empty, Field, Notice, Panel, Tag } from '../ui';

export function BotAPIVariablesPanel({ variables, draft, onDraftChange, onSave, onDelete }: { variables: BotAPIVariablesResponse | null; draft: APIVariableDraft; onDraftChange: (draft: APIVariableDraft) => void; onSave: () => void; onDelete: (variable: BotAPIVariable) => void }) {
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
                <Tag tone={variable.enabled ? 'ok' : 'neutral'}>{variable.enabled ? 'enabled' : 'paused'}</Tag>
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

