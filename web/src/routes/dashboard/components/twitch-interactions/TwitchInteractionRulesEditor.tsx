import { useMemo, useState, type Dispatch, type SetStateAction } from 'react';
import { saveTwitchInteractions, type TwitchInteractionRules } from '../../../../lib/api';
import {
  aliasCommands,
  createEmptyTwitchInteractionRules,
  normalizeImportedTwitchInteractionRules,
  rewardActions,
  updateTwitchAliasRule,
  updateTwitchRewardRule,
} from '../../../../lib/twitchInteractionRules';
import type { ChatAliasRule, ChannelPointRule } from '../../../../lib/api';
import { Check, Empty, Field, Notice, SubPanel } from '../../ui';

type TwitchInteractionRulesEditorProps = {
  apiBaseUrl?: string;
  operatorSecret: string;
  rules: TwitchInteractionRules;
  onRulesChange: Dispatch<SetStateAction<TwitchInteractionRules>>;
  loadError: string;
  onLoadErrorChange: Dispatch<SetStateAction<string>>;
};

export function TwitchInteractionRulesEditor({ apiBaseUrl, operatorSecret, rules, onRulesChange, loadError, onLoadErrorChange }: TwitchInteractionRulesEditorProps) {
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [statusMessage, setStatusMessage] = useState('');
  const [importRaw, setImportRaw] = useState('');
  const [importError, setImportError] = useState('');

  const exportJson = useMemo(() => JSON.stringify(rules, null, 2), [rules]);

  function addAlias() {
    onRulesChange((current) => ({ ...current, chatAliases: [...current.chatAliases, { alias: '', command: 'alert', enabled: true }] }));
  }

  function addReward() {
    onRulesChange((current) => ({
      ...current,
      channelPointRules: [...current.channelPointRules, { rewardTitle: '', action: 'ignore', enabled: true }],
    }));
  }

  function handleImport() {
    try {
      const parsed = JSON.parse(importRaw) as unknown;
      onRulesChange(normalizeImportedTwitchInteractionRules(parsed));
      setImportError('');
      setStatusMessage('Imported backend rules JSON.');
    } catch {
      setImportError('Import failed: invalid rules JSON.');
    }
  }

  async function handleSave() {
    if (!operatorSecret.trim()) {
      setSaveError('Operator secret required to save.');
      return;
    }

    setSaving(true);
    setSaveError('');
    setStatusMessage('');

    try {
      const response = await saveTwitchInteractions(rules, apiBaseUrl, operatorSecret);
      onRulesChange(response.rules ?? createEmptyTwitchInteractionRules());
      onLoadErrorChange(response.loadError ?? '');
      setStatusMessage('Saved Twitch interaction rules.');
    } catch (error: unknown) {
      setSaveError(error instanceof Error ? error.message : 'Failed to save Twitch interaction rules.');
    } finally {
      setSaving(false);
    }
  }

  return (
    <>
      <div className="grid gap-4 xl:grid-cols-2">
        <SubPanel title="Chat aliases">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="desk-row__text">Map a chat bang to one of the built-in commands.</p>
            <button className="desk-btn desk-btn--small" type="button" onClick={addAlias}>
              Add alias
            </button>
          </div>

          {rules.chatAliases.length > 0 ? (
            <ul className="desk-rows">
              {rules.chatAliases.map((rule, index) => (
                <li key={`${rule.alias || 'alias'}-${index}`} className={`desk-row ${rule.enabled ? '' : 'desk-row--warn'}`}>
                  <div className="grid gap-2 md:grid-cols-[1fr_10rem_auto] md:items-end">
                    <Field label="Alias">
                      <input className="desk-input" type="text" value={rule.alias} onChange={(event) => onRulesChange((current) => updateTwitchAliasRule(current, index, { alias: event.target.value }))} placeholder="panic" />
                    </Field>
                    <Field label="Command">
                      <select className="desk-input" value={rule.command} onChange={(event) => onRulesChange((current) => updateTwitchAliasRule(current, index, { command: event.target.value as ChatAliasRule['command'] }))}>
                        {aliasCommands.map((command) => (
                          <option key={command} value={command}>
                            {command}
                          </option>
                        ))}
                      </select>
                    </Field>
                    <div className="flex min-h-8 items-center gap-3">
                      <Check label="Enabled" checked={rule.enabled} onChange={(checked) => onRulesChange((current) => updateTwitchAliasRule(current, index, { enabled: checked }))} />
                      <button className="desk-btn desk-btn--small" type="button" onClick={() => onRulesChange((current) => ({ ...current, chatAliases: current.chatAliases.filter((_, itemIndex) => itemIndex !== index) }))}>
                        Remove
                      </button>
                    </div>
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <Empty>No chat aliases yet.</Empty>
          )}
        </SubPanel>

        <SubPanel title="Channel point rewards">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="desk-row__text">An exact reward title match can alert, show BRB, clear, or be ignored. Cooldowns and fulfillment notes are optional.</p>
            <button className="desk-btn desk-btn--small" type="button" onClick={addReward}>
              Add reward
            </button>
          </div>

          {rules.channelPointRules.length > 0 ? (
            <ul className="desk-rows">
              {rules.channelPointRules.map((rule, index) => (
                <li key={`${rule.rewardTitle || 'reward'}-${index}`} className={`desk-row ${rule.enabled ? '' : 'desk-row--warn'}`}>
                  <div className="grid gap-2 md:grid-cols-2">
                    <Field label="Reward title" className="md:col-span-2">
                      <input className="desk-input" type="text" value={rule.rewardTitle} onChange={(event) => onRulesChange((current) => updateTwitchRewardRule(current, index, { rewardTitle: event.target.value }))} placeholder="Hydrate break" />
                    </Field>
                    <Field label="Action">
                      <select className="desk-input" value={rule.action} onChange={(event) => onRulesChange((current) => updateTwitchRewardRule(current, index, { action: event.target.value as ChannelPointRule['action'] }))}>
                        {rewardActions.map((action) => (
                          <option key={action} value={action}>
                            {action}
                          </option>
                        ))}
                      </select>
                    </Field>
                    <Field label="Cooldown seconds">
                      <input className="desk-input" type="number" min={0} max={86400} value={rule.cooldownSeconds ?? 0} onChange={(event) => onRulesChange((current) => updateTwitchRewardRule(current, index, { cooldownSeconds: Number(event.target.value) }))} />
                    </Field>
                    <Field label="Title override" className="md:col-span-2">
                      <input className="desk-input" type="text" value={rule.title ?? ''} onChange={(event) => onRulesChange((current) => updateTwitchRewardRule(current, index, { title: event.target.value }))} placeholder="Optional alert title" />
                    </Field>
                    <Field label="Message override" className="md:col-span-2">
                      <textarea className="desk-input desk-input--mono" value={rule.message ?? ''} onChange={(event) => onRulesChange((current) => updateTwitchRewardRule(current, index, { message: event.target.value }))} placeholder="Optional alert message" />
                    </Field>
                    <Field label="Fulfillment note" className="md:col-span-2">
                      <input className="desk-input" type="text" value={rule.fulfillmentNote ?? ''} onChange={(event) => onRulesChange((current) => updateTwitchRewardRule(current, index, { fulfillmentNote: event.target.value }))} placeholder="Optional operator note" />
                    </Field>
                    <div className="flex items-center justify-between gap-3 md:col-span-2">
                      <Check label="Enabled" checked={rule.enabled} onChange={(checked) => onRulesChange((current) => updateTwitchRewardRule(current, index, { enabled: checked }))} />
                      <button className="desk-btn desk-btn--small" type="button" onClick={() => onRulesChange((current) => ({ ...current, channelPointRules: current.channelPointRules.filter((_, itemIndex) => itemIndex !== index) }))}>
                        Remove
                      </button>
                    </div>
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <Empty>No channel point reward rules yet.</Empty>
          )}
        </SubPanel>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <SubPanel title="Import backend rules JSON">
          <textarea className="desk-input desk-input--mono min-h-32" value={importRaw} onChange={(event) => setImportRaw(event.target.value)} placeholder="Paste chatAliases/channelPointRules JSON here" />
          <div className="flex flex-wrap items-center gap-3">
            <button className="desk-btn" type="button" onClick={handleImport}>
              Import rules JSON
            </button>
          </div>
          {importError ? <Notice tone="warn" role="alert">{importError}</Notice> : null}
        </SubPanel>

        <SubPanel title="Export backend rules JSON">
          <textarea className="desk-input desk-input--mono min-h-32" value={exportJson} readOnly />
          <div className="flex flex-wrap items-center gap-3">
            <button className="desk-btn desk-btn--primary" type="button" onClick={handleSave} disabled={saving}>
              {saving ? 'Saving…' : 'Save rules'}
            </button>
            <span className="desk-field__hint">Operator secret required to save.</span>
          </div>
          {saveError ? <Notice tone="bad" role="alert">{saveError}</Notice> : null}
          {statusMessage ? <Notice tone="ok" role="status">{statusMessage}</Notice> : null}
          {loadError ? <Notice tone="warn">{loadError}</Notice> : null}
        </SubPanel>
      </div>
    </>
  );
}
