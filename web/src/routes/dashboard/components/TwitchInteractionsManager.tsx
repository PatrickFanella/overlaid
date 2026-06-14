import { useEffect, useMemo, useState } from 'react';
import {
  fetchTwitchInteractions,
  saveTwitchInteractions,
  type ChatAliasRule,
  type ChannelPointRule,
  type TwitchInteractionRules,
} from '../../../lib/api';

type TwitchInteractionsManagerProps = {
  apiBaseUrl?: string;
  operatorSecret: string;
};

const aliasCommands = ['alert', 'clear', 'brb', 'back'] as const;
const rewardActions = ['alert', 'brb', 'clear', 'ignore'] as const;

function createEmptyRules(): TwitchInteractionRules {
  return { chatAliases: [], channelPointRules: [] };
}

function cloneRules(rules: TwitchInteractionRules): TwitchInteractionRules {
  return {
    chatAliases: rules.chatAliases.map((rule) => ({ ...rule })),
    channelPointRules: rules.channelPointRules.map((rule) => ({ ...rule })),
  };
}

function isRules(value: unknown): value is TwitchInteractionRules {
  if (typeof value !== 'object' || value === null) return false;
  const record = value as Record<string, unknown>;
  return Array.isArray(record.chatAliases) && Array.isArray(record.channelPointRules);
}

function normalizeImportedRules(value: unknown): TwitchInteractionRules {
  if (!isRules(value)) {
    throw new Error('invalid rules JSON');
  }

  const chatAliases = value.chatAliases
    .filter((entry): entry is ChatAliasRule => typeof entry === 'object' && entry !== null && typeof entry.alias === 'string' && typeof entry.command === 'string' && typeof entry.enabled === 'boolean')
    .map((entry) => ({ alias: entry.alias, command: aliasCommands.includes(entry.command as (typeof aliasCommands)[number]) ? (entry.command as ChatAliasRule['command']) : 'alert', enabled: entry.enabled }));
  const channelPointRules = value.channelPointRules
    .filter(
      (entry): entry is ChannelPointRule =>
        typeof entry === 'object' &&
        entry !== null &&
        typeof entry.rewardTitle === 'string' &&
        typeof entry.action === 'string' &&
        typeof entry.enabled === 'boolean',
    )
    .map((entry) => ({
      rewardTitle: entry.rewardTitle,
      action: rewardActions.includes(entry.action as (typeof rewardActions)[number]) ? (entry.action as ChannelPointRule['action']) : 'ignore',
      title: typeof entry.title === 'string' ? entry.title : undefined,
      message: typeof entry.message === 'string' ? entry.message : undefined,
      enabled: entry.enabled,
    }));

  return { chatAliases, channelPointRules };
}

function updateAlias(rules: TwitchInteractionRules, index: number, patch: Partial<ChatAliasRule>) {
  const next = cloneRules(rules);
  next.chatAliases[index] = { ...next.chatAliases[index], ...patch };
  return next;
}

function updateReward(rules: TwitchInteractionRules, index: number, patch: Partial<ChannelPointRule>) {
  const next = cloneRules(rules);
  next.channelPointRules[index] = { ...next.channelPointRules[index], ...patch };
  return next;
}

export function TwitchInteractionsManager({ apiBaseUrl, operatorSecret }: TwitchInteractionsManagerProps) {
  const [rules, setRules] = useState<TwitchInteractionRules>(() => createEmptyRules());
  const [loadError, setLoadError] = useState('');
  const [statusMessage, setStatusMessage] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [importRaw, setImportRaw] = useState('');
  const [importError, setImportError] = useState('');

  useEffect(() => {
    let cancelled = false;

    setLoading(true);
    fetchTwitchInteractions({ baseUrl: apiBaseUrl, operatorSecret })
      .then((response) => {
        if (cancelled) return;
        setRules(response.rules ?? createEmptyRules());
        setLoadError(response.loadError ?? '');
      })
      .catch((error: unknown) => {
        if (cancelled) return;
        setLoadError(error instanceof Error ? error.message : 'Failed to load Twitch interaction rules.');
      })
      .finally(() => {
        if (cancelled) return;
        setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [apiBaseUrl, operatorSecret]);

  const exportJson = useMemo(() => JSON.stringify(rules, null, 2), [rules]);

  function addAlias() {
    setRules((current) => ({ ...current, chatAliases: [...current.chatAliases, { alias: '', command: 'alert', enabled: true }] }));
  }

  function addReward() {
    setRules((current) => ({
      ...current,
      channelPointRules: [...current.channelPointRules, { rewardTitle: '', action: 'ignore', enabled: true }],
    }));
  }

  function handleImport() {
    try {
      const parsed = JSON.parse(importRaw) as unknown;
      setRules(normalizeImportedRules(parsed));
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
      setRules(response.rules ?? createEmptyRules());
      setLoadError(response.loadError ?? '');
      setStatusMessage('Saved Twitch interaction rules.');
    } catch (error: unknown) {
      setSaveError(error instanceof Error ? error.message : 'Failed to save Twitch interaction rules.');
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <p className="hud-label">Twitch interaction rules</p>
          <h2 className="hud-section-title">Twitch interaction rules</h2>
          <p className="hud-copy mt-1 text-xs text-zinc-300">Chat aliases and channel point routing live on the backend. Operator secret required to save.</p>
        </div>
        <span className="hud-chip">{loading ? 'Loading rules…' : 'Rules loaded'}</span>
      </div>

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
                    <input className="hud-input" type="text" value={rule.alias} onChange={(event) => setRules((current) => updateAlias(current, index, { alias: event.target.value }))} placeholder="panic" />
                  </label>
                  <label className="grid gap-1 text-xs text-zinc-300">
                    <span className="hud-kicker">Command</span>
                    <select className="hud-input" value={rule.command} onChange={(event) => setRules((current) => updateAlias(current, index, { command: event.target.value as ChatAliasRule['command'] }))}>
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
                      <input type="checkbox" checked={rule.enabled} onChange={(event) => setRules((current) => updateAlias(current, index, { enabled: event.target.checked }))} />
                    </label>
                    <button className="hud-chip" type="button" onClick={() => setRules((current) => ({ ...current, chatAliases: current.chatAliases.filter((_, itemIndex) => itemIndex !== index) }))}>
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
              <p className="mt-1 text-xs text-zinc-400">Exact title matches can alert, BRB, clear, or ignore.</p>
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
                      <input className="hud-input" type="text" value={rule.rewardTitle} onChange={(event) => setRules((current) => updateReward(current, index, { rewardTitle: event.target.value }))} placeholder="Hydrate break" />
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300">
                      <span className="hud-kicker">Action</span>
                      <select className="hud-input" value={rule.action} onChange={(event) => setRules((current) => updateReward(current, index, { action: event.target.value as ChannelPointRule['action'] }))}>
                        {rewardActions.map((action) => (
                          <option key={action} value={action}>
                            {action}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300">
                      <span className="hud-kicker">Enabled</span>
                      <input type="checkbox" checked={rule.enabled} onChange={(event) => setRules((current) => updateReward(current, index, { enabled: event.target.checked }))} />
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300 md:col-span-2">
                      <span className="hud-kicker">Title override</span>
                      <input className="hud-input" type="text" value={rule.title ?? ''} onChange={(event) => setRules((current) => updateReward(current, index, { title: event.target.value }))} placeholder="Optional alert title" />
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300 md:col-span-2">
                      <span className="hud-kicker">Message override</span>
                      <textarea className="hud-input min-h-20 font-mono text-xs" value={rule.message ?? ''} onChange={(event) => setRules((current) => updateReward(current, index, { message: event.target.value }))} placeholder="Optional alert message" />
                    </label>
                  </div>
                  <button className="hud-chip self-start" type="button" onClick={() => setRules((current) => ({ ...current, channelPointRules: current.channelPointRules.filter((_, itemIndex) => itemIndex !== index) }))}>
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
    </section>
  );
}
