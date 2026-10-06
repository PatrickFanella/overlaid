import type { BotModerationRule } from '../../../lib/api';
import type { ModerationActionDraft, ModerationDraft, NukeDraft } from './types';
import type { BotModerationState } from './useBotModeration';
import { Check, Empty, Field, Notice, Panel, SubPanel, Tag, type Tone } from '../ui';

function ruleState(rule: BotModerationRule): { label: string; tone: Tone } {
  if (rule.expiresAt && new Date(rule.expiresAt).getTime() <= Date.now()) return { label: 'expired', tone: 'neutral' };
  if (!rule.enabled) return { label: 'paused', tone: 'neutral' };
  return rule.enforce ? { label: 'enforced', tone: 'warn' } : { label: 'dry-run', tone: 'ok' };
}

export function BotModerationPanel({
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
  onSave, onEdit,
  onDelete,
  onDryRun,
  onAction,
  onNukeDryRun,
  onNukeExecute,
  onNukeRollback,
  loading, pending, error, result, nukeError,
}: BotModerationState) {
  const busy = Boolean(pending);
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
        {loading ? <p className="desk-note">Loading moderation rules…</p> : null}
        {error ? <Notice tone="bad" role="alert">{error}</Notice> : null}
        {result ? <Notice tone="ok" role="status">{result}</Notice> : null}
        <fieldset disabled={busy} className="contents">
          {rules?.loadError ? <Notice tone="warn">Moderation unavailable: {rules.loadError}</Notice> : null}

          <fieldset disabled={loading} className="contents">
            <SubPanel title={draft.id ? "Edit rule" : "New rule"}>
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
              <div className="grid gap-3 md:grid-cols-2">
                <Field label="Exempt role and above"><select className="desk-input" value={draft.exemptRole} onChange={event => onDraftChange({ ...draft, exemptRole: event.target.value })}>
                  <option value="moderator">Moderator</option><option value="broadcaster">Broadcaster</option><option value="viewer">Everyone</option><option value="none">None</option>
                </select></Field>
                <Field label="Rule timeout seconds"><input className="desk-input" type="number" min={1} max={1209600} value={draft.timeoutSeconds} onChange={event => onDraftChange({ ...draft, timeoutSeconds: Number(event.target.value) })} /></Field>
              </div>
              <Field label="Reason" hint="Shown in audit and planning.">
                <input className="desk-input" value={draft.reason} onChange={(event) => onDraftChange({ ...draft, reason: event.target.value })} placeholder="reason shown in audit/planning" />
              </Field>
              <div className="flex flex-wrap items-center gap-3">
                <Check label="Enabled" checked={draft.enabled} onChange={(enabled) => onDraftChange({ ...draft, enabled })} />
                <Check label="Enforce" checked={draft.enforce} onChange={(enforce) => onDraftChange({ ...draft, enforce })} />
                <Check label="Regex" checked={draft.regex} onChange={(regex) => onDraftChange({ ...draft, regex })} />
                <Check label="Case sensitive" checked={draft.caseSensitive} onChange={(caseSensitive) => onDraftChange({ ...draft, caseSensitive })} />
                <button className="desk-btn desk-btn--primary" type="button" disabled={!canSave || loading} onClick={onSave}>
                  {pending === 'save' ? 'Saving…' : 'Save rule'}
                </button>
              </div>
            </SubPanel>

          </fieldset>
          <SubPanel title="Test a message">
            <div className="grid items-end gap-3 md:grid-cols-[10rem_1fr_auto]">
              <Field label="Username">
                <input className="desk-input" value={test.username} onChange={(event) => onTestChange({ ...test, username: event.target.value })} placeholder="username" />
              </Field>
              <Field label="Test role"><select className="desk-input" value={test.role} onChange={event => onTestChange({ ...test, role: event.target.value })}><option value="viewer">Viewer</option><option value="moderator">Moderator</option><option value="broadcaster">Broadcaster</option></select></Field>
              <Field label="Message to test">
                <input className="desk-input" value={test.message} onChange={(event) => onTestChange({ ...test, message: event.target.value })} placeholder="message to test" />
              </Field>
              <button className="desk-btn" type="button" onClick={onDryRun}>
                {pending === 'dry-run' ? 'Testing…' : 'Dry run'}
              </button>
            </div>
            {dryRun ? (
              <p className="desk-row__text">
                Matches: <span className="mono">{dryRun.matches.length ? dryRun.matches.map((match) => `${match.ruleName}:${match.action}${match.skipped ? ` (skipped: ${match.note})` : ''}`).join(', ') : 'none'}</span>
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
                {pending === 'action' ? 'Running…' : 'Run guarded action'}
              </button>
              {actionResult ? <Tag tone={actionResult.result === 'allowed' ? 'ok' : 'bad'}>{actionResult.action} {actionResult.result}{actionResult.reason ? ` · ${actionResult.reason}` : ''}</Tag> : null}
            </div>
          </SubPanel>

          {!loading && !rules?.loadError && items.length === 0 ? (
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
                      Exempt {rule.exemptRole || 'moderator'} and above · timeout {rule.timeoutSeconds || 60}s · {rule.matchTarget} · {rule.regex ? 'regex' : 'contains'} · {rule.action}
                    </p>
                    <p className="desk-row__text mono">{rule.pattern}</p>
                    <div className="desk-row__line">
                      <span className="desk-row__meta">{rule.reason || 'no reason'}</span>
                      <button className="desk-btn desk-btn--small" type="button" disabled={loading} onClick={() => onEdit(rule)}>Edit</button>
                      <button className="desk-btn desk-btn--small desk-btn--danger" type="button" disabled={loading} onClick={() => onDelete(rule)}>
                        Delete
                      </button>
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </fieldset>
      </Panel>

      <Panel
        id="bot-nuke"
        danger
        kicker="Destructive"
        title="Nuke dry-run planner"
        description="Dry-run first. Execute requires confirmation, is capped, and only delete and timeout are supported."
      >
        {nukeError ? <Notice tone="bad" role="alert">{nukeError}</Notice> : null}
        <fieldset disabled={busy} className="contents">
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
                <input className="desk-input" type="number" min={1} max={1209600} value={nukeDraft.durationSeconds} onChange={(event) => onNukeDraftChange({ ...nukeDraft, durationSeconds: Number(event.target.value), confirm: false })} />
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
                {pending === 'nuke-plan' ? 'Planning…' : 'Plan nuke'}
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
            <fieldset disabled={!nukeResult?.matches.length} className="contents"><Check label="I reviewed this dry-run plan" checked={nukeDraft.confirm} onChange={(confirm) => onNukeDraftChange({ ...nukeDraft, confirm })} /></fieldset>
          </SubPanel>

          <SubPanel title="3. Execute">
            <div>
              <button className="desk-btn desk-btn--danger" type="button" disabled={!canExecuteNuke} onClick={onNukeExecute}>
                {pending === 'nuke-execute' ? 'Executing…' : 'Execute planned nuke'}
              </button>
            </div>
            {nukeExecuteResult ? (
              <Notice tone="warn">
                Executed {nukeExecuteResult.action}: {nukeExecuteResult.succeeded}/{nukeExecuteResult.attempted} succeeded · {nukeExecuteResult.failed} failed
                {nukeExecuteResult.interrupted ? ` · execution interrupted; ${nukeExecuteResult.matches.length - nukeExecuteResult.attempted} not attempted` : ''}
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
                        {pending === 'nuke-rollback' ? 'Rolling back…' : 'Rollback window'}
                      </button>
                    </div>
                  </li>
                ))}
              </ul>
            </SubPanel>
          ) : null}
        </fieldset>
      </Panel>
    </>
  );
}

