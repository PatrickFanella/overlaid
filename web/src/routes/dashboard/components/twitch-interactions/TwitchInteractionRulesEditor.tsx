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
      <div className="mt-3 grid gap-3 xl:grid-cols-2">
        <div className="hud-panel hud-corners hud-panel--flat hud-compact-card">
          <div className="flex items-center justify-between gap-3">
            <div>
              <p className="hud-kicker">Chat aliases</p>
              <p className="mt-1 text-xs text-zinc-400">Alias bangs to safe built-in commands only.</p>
            </div>
            <button className="hud-chip" type="button" onClick={addAlias}>
              Add alias
            </button>
          </div>

          <div className="mt-3 grid gap-2">
            {rules.chatAliases.length > 0 ? (
              rules.chatAliases.map((rule, index) => (
                <article key={`${rule.alias || 'alias'}-${index}`} className="grid gap-2 rounded-2xl border border-cyan-400/20 bg-cyan-400/5 p-3 md:grid-cols-[1fr_10rem_auto] md:items-end">
                  <label className="grid gap-1 text-xs text-zinc-300">
                    <span className="hud-kicker">Alias</span>
                    <input className="hud-input" type="text" value={rule.alias} onChange={(event) => onRulesChange((current) => updateTwitchAliasRule(current, index, { alias: event.target.value }))} placeholder="panic" />
                  </label>
                  <label className="grid gap-1 text-xs text-zinc-300">
                    <span className="hud-kicker">Command</span>
                    <select className="hud-input" value={rule.command} onChange={(event) => onRulesChange((current) => updateTwitchAliasRule(current, index, { command: event.target.value as ChatAliasRule['command'] }))}>
                      {aliasCommands.map((command) => (
                        <option key={command} value={command}>
                          {command}
                        </option>
                      ))}
                    </select>
                  </label>
                  <div className="flex items-center gap-2">
                    <label className="grid gap-1 text-xs text-zinc-300">
                      <span className="hud-kicker">Enabled</span>
                      <input type="checkbox" checked={rule.enabled} onChange={(event) => onRulesChange((current) => updateTwitchAliasRule(current, index, { enabled: event.target.checked }))} />
                    </label>
                    <button className="hud-chip" type="button" onClick={() => onRulesChange((current) => ({ ...current, chatAliases: current.chatAliases.filter((_, itemIndex) => itemIndex !== index) }))}>
                      Remove
                    </button>
                  </div>
                </article>
              ))
            ) : (
              <p className="text-xs text-zinc-400">No chat aliases yet.</p>
            )}
          </div>
        </div>

        <div className="hud-panel hud-corners hud-panel--flat hud-compact-card">
          <div className="flex items-center justify-between gap-3">
            <div>
              <p className="hud-kicker">Channel point rewards</p>
              <p className="mt-1 text-xs text-zinc-400">Exact title matches can alert, BRB, clear, or ignore, with optional cooldowns and fulfillment notes.</p>
            </div>
            <button className="hud-chip" type="button" onClick={addReward}>
              Add reward
            </button>
          </div>

          <div className="mt-3 grid gap-2">
            {rules.channelPointRules.length > 0 ? (
              rules.channelPointRules.map((rule, index) => (
                <article key={`${rule.rewardTitle || 'reward'}-${index}`} className="grid gap-2 rounded-2xl border border-fuchsia-400/20 bg-fuchsia-400/5 p-3 xl:grid-cols-[1fr_10rem]">
                  <div className="grid gap-2 md:grid-cols-2 xl:col-span-2">
                    <label className="grid gap-1 text-xs text-zinc-300 md:col-span-2">
                      <span className="hud-kicker">Reward title</span>
                      <input className="hud-input" type="text" value={rule.rewardTitle} onChange={(event) => onRulesChange((current) => updateTwitchRewardRule(current, index, { rewardTitle: event.target.value }))} placeholder="Hydrate break" />
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300">
                      <span className="hud-kicker">Action</span>
                      <select className="hud-input" value={rule.action} onChange={(event) => onRulesChange((current) => updateTwitchRewardRule(current, index, { action: event.target.value as ChannelPointRule['action'] }))}>
                        {rewardActions.map((action) => (
                          <option key={action} value={action}>
                            {action}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300">
                      <span className="hud-kicker">Enabled</span>
                      <input type="checkbox" checked={rule.enabled} onChange={(event) => onRulesChange((current) => updateTwitchRewardRule(current, index, { enabled: event.target.checked }))} />
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300">
                      <span className="hud-kicker">Cooldown seconds</span>
                      <input className="hud-input" type="number" min={0} max={86400} value={rule.cooldownSeconds ?? 0} onChange={(event) => onRulesChange((current) => updateTwitchRewardRule(current, index, { cooldownSeconds: Number(event.target.value) }))} />
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300 md:col-span-2">
                      <span className="hud-kicker">Title override</span>
                      <input className="hud-input" type="text" value={rule.title ?? ''} onChange={(event) => onRulesChange((current) => updateTwitchRewardRule(current, index, { title: event.target.value }))} placeholder="Optional alert title" />
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300 md:col-span-2">
                      <span className="hud-kicker">Message override</span>
                      <textarea className="hud-input min-h-20 font-mono text-xs" value={rule.message ?? ''} onChange={(event) => onRulesChange((current) => updateTwitchRewardRule(current, index, { message: event.target.value }))} placeholder="Optional alert message" />
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300 md:col-span-2">
                      <span className="hud-kicker">Fulfillment note</span>
                      <input className="hud-input" type="text" value={rule.fulfillmentNote ?? ''} onChange={(event) => onRulesChange((current) => updateTwitchRewardRule(current, index, { fulfillmentNote: event.target.value }))} placeholder="Optional operator note" />
                    </label>
                  </div>
                  <button className="hud-chip self-start" type="button" onClick={() => onRulesChange((current) => ({ ...current, channelPointRules: current.channelPointRules.filter((_, itemIndex) => itemIndex !== index) }))}>
                    Remove
                  </button>
                </article>
              ))
            ) : (
              <p className="text-xs text-zinc-400">No channel point reward rules yet.</p>
            )}
          </div>
        </div>
      </div>

      <div className="mt-3 grid gap-3 lg:grid-cols-2">
        <div className="hud-panel hud-corners hud-panel--flat hud-compact-card">
          <p className="hud-kicker">Import backend rules JSON</p>
          <textarea className="hud-input mt-3 min-h-32 font-mono text-xs" value={importRaw} onChange={(event) => setImportRaw(event.target.value)} placeholder="Paste chatAliases/channelPointRules JSON here" />
          <div className="mt-3 flex flex-wrap items-center gap-3">
            <button className="hud-chip" type="button" onClick={handleImport}>
              Import rules JSON
            </button>
            {importError ? <p className="text-xs text-amber-200">{importError}</p> : null}
          </div>
        </div>

        <div className="hud-panel hud-corners hud-panel--flat hud-compact-card">
          <p className="hud-kicker">Export backend rules JSON</p>
          <textarea className="hud-input mt-3 min-h-32 font-mono text-xs" value={exportJson} readOnly />
          <div className="mt-3 flex flex-wrap items-center gap-3">
            <button className="hud-chip" type="button" onClick={handleSave} disabled={saving}>
              {saving ? 'Saving…' : 'Save rules'}
            </button>
            <span className="text-xs text-zinc-400">Operator secret required to save.</span>
          </div>
          {saveError ? <p className="mt-2 text-xs text-amber-200">{saveError}</p> : null}
          {statusMessage ? <p className="mt-2 text-xs text-emerald-200">{statusMessage}</p> : null}
          {loadError ? <p className="mt-2 text-xs text-amber-200">{loadError}</p> : null}
        </div>
      </div>
    </>
  );
}
