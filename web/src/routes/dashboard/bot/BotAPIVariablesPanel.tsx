import type { BotAPIVariablesState } from './useBotAPIVariables';
import { Check, Empty, Field, Notice, Panel, Tag } from '../ui';

export function BotAPIVariablesPanel({ variables, draft, loading, pending, error, result, onDraftChange, onSave, onDelete, onEdit, onCancel }: BotAPIVariablesState) {
  const items = variables?.variables ?? [];
  const busy = loading || Boolean(pending);
  const canSave = /^[a-z0-9][a-z0-9_-]{0,31}$/.test(draft.name.trim().toLowerCase()) && draft.url.trim().startsWith('https://') && Number.isInteger(draft.cacheTtlSeconds) && draft.cacheTtlSeconds >= 15 && draft.cacheTtlSeconds <= 3600 && Number.isInteger(draft.timeoutMs) && draft.timeoutMs >= 250 && draft.timeoutMs <= 3000;
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
      {loading ? <p className="desk-note">Loading API variables…</p> : null}
      {error ? <Notice tone="bad" role="alert">{error}</Notice> : null}
      {result ? <Notice tone="ok" role="status">{result}</Notice> : null}
      {draft.id ? <p className="desk-note">Editing API variable {draft.id}.</p> : null}
      {variables?.loadError ? <Notice tone="warn">API variables unavailable: {variables.loadError}</Notice> : null}
      <div className="grid gap-3 md:grid-cols-[1fr_2fr_10rem_8rem]">
        <Field label="Name">
          <input className="desk-input desk-input--mono" disabled={busy} value={draft.name} onChange={(event) => onDraftChange({ ...draft, name: event.target.value })} placeholder="weather" />
        </Field>
        <Field label="HTTPS URL">
          <input className="desk-input desk-input--mono" disabled={busy} value={draft.url} onChange={(event) => onDraftChange({ ...draft, url: event.target.value })} placeholder="https://api.example.test/value" />
        </Field>
        <Field label="JSON selector">
          <input className="desk-input desk-input--mono" disabled={busy} value={draft.responseSelector} onChange={(event) => onDraftChange({ ...draft, responseSelector: event.target.value })} placeholder="data.value" />
        </Field>
        <Field label="TTL seconds">
          <input className="desk-input" type="number" min={15} max={3600} step={1} disabled={busy} value={draft.cacheTtlSeconds} onChange={(event) => onDraftChange({ ...draft, cacheTtlSeconds: Number(event.target.value) })} />
        </Field>
      </div>
      <div className="grid gap-3 md:grid-cols-[1fr_8rem]">
        <Field label="Fallback">
          <input className="desk-input" disabled={busy} value={draft.fallback} onChange={(event) => onDraftChange({ ...draft, fallback: event.target.value })} placeholder="unavailable" />
        </Field>
        <Field label="Timeout ms">
          <input className="desk-input" type="number" min={250} max={3000} step={1} disabled={busy} value={draft.timeoutMs} onChange={(event) => onDraftChange({ ...draft, timeoutMs: Number(event.target.value) })} />
        </Field>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <fieldset disabled={busy}><Check label="Enabled" checked={draft.enabled} onChange={(enabled) => onDraftChange({ ...draft, enabled })} /></fieldset>
        <button className="desk-btn desk-btn--primary" type="button" disabled={!canSave || busy} onClick={onSave}>
          {pending?.action === 'save' ? 'Saving…' : draft.id ? 'Save changes' : 'Save API variable'}
        </button>
        {draft.id ? <button className="desk-btn" type="button" disabled={busy} onClick={onCancel}>Cancel edit</button> : null}
      </div>
      {!loading && !variables?.loadError && items.length === 0 ? (
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
                <button className="desk-btn desk-btn--small" type="button" disabled={busy} onClick={() => onEdit(variable)}>Edit</button>
                <button className="desk-btn desk-btn--small desk-btn--danger" type="button" disabled={busy} onClick={() => onDelete(variable)}>
                  {pending?.action === 'delete' && pending.id === variable.id ? 'Deleting…' : 'Delete'}
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

