import { useEffect, useMemo, useState } from 'react';
import {
  createTwitchReward,
  deleteTwitchReward,
  fetchTwitchInteractions,
  fetchTwitchRewardRedemptions,
  fetchTwitchRewards,
  saveTwitchInteractions,
  updateTwitchReward,
  updateTwitchRedemptionStatus,
  type ChatAliasRule,
  type ChannelPointRule,
  type TwitchRedemption,
  type TwitchReward,
  type TwitchInteractionRules,
} from '../../../lib/api';
import {
  aliasCommands,
  createEmptyTwitchInteractionRules,
  findMatchingChannelPointRule,
  normalizeImportedTwitchInteractionRules,
  rewardActions,
  updateTwitchAliasRule,
  updateTwitchRewardRule,
} from '../../../lib/twitchInteractionRules';

type TwitchInteractionsManagerProps = {
  apiBaseUrl?: string;
  operatorSecret: string;
};

export function TwitchInteractionsManager({ apiBaseUrl, operatorSecret }: TwitchInteractionsManagerProps) {
  const [rules, setRules] = useState<TwitchInteractionRules>(() => createEmptyTwitchInteractionRules());
  const [loadError, setLoadError] = useState('');
  const [statusMessage, setStatusMessage] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [importRaw, setImportRaw] = useState('');
  const [importError, setImportError] = useState('');
  const [rewards, setRewards] = useState<TwitchReward[]>([]);
  const [redemptions, setRedemptions] = useState<TwitchRedemption[]>([]);
  const [selectedRewardId, setSelectedRewardId] = useState('');
  const [rewardError, setRewardError] = useState('');
  const [rewardDraft, setRewardDraft] = useState({ title: '', prompt: '', cost: 100, enabled: false, userInputRequired: false });
  const [deleteRewardConfirmed, setDeleteRewardConfirmed] = useState(false);
  const selectedReward = useMemo(() => rewards.find((reward) => reward.id === selectedRewardId), [rewards, selectedRewardId]);
  const selectedRewardRule = useMemo(() => (selectedReward ? findMatchingChannelPointRule(rules.channelPointRules, selectedReward.title) : undefined), [rules.channelPointRules, selectedReward]);
  const redemptionsWithRules = useMemo(
    () => redemptions.map((redemption) => ({ redemption, matchedRule: findMatchingChannelPointRule(rules.channelPointRules, redemption.reward.title) })),
    [redemptions, rules.channelPointRules],
  );

  useEffect(() => {
    let cancelled = false;

    setLoading(true);
    fetchTwitchInteractions({ baseUrl: apiBaseUrl, operatorSecret })
      .then((response) => {
        if (cancelled) return;
        setRules(response.rules ?? createEmptyTwitchInteractionRules());
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
      setRules(normalizeImportedTwitchInteractionRules(parsed));
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
      setRules(response.rules ?? createEmptyTwitchInteractionRules());
      setLoadError(response.loadError ?? '');
      setStatusMessage('Saved Twitch interaction rules.');
    } catch (error: unknown) {
      setSaveError(error instanceof Error ? error.message : 'Failed to save Twitch interaction rules.');
    } finally {
      setSaving(false);
    }
  }

  async function loadRewards(preferredRewardId = selectedRewardId) {
    try {
      const response = await fetchTwitchRewards(apiBaseUrl, operatorSecret);
      const loadedRewards = response.rewards ?? [];
      const nextSelectedRewardId = loadedRewards.find((reward) => reward.id === preferredRewardId)?.id ?? loadedRewards[0]?.id ?? '';
      setRewards(loadedRewards);
      setSelectedRewardId(nextSelectedRewardId);
      setDeleteRewardConfirmed(false);
      setRewardError('');
    } catch (error) {
      setRewardError(error instanceof Error ? error.message : 'Failed to load rewards.');
    }
  }

  async function loadRedemptions(rewardId = selectedRewardId) {
    if (!rewardId) return;
    try {
      const response = await fetchTwitchRewardRedemptions(rewardId, apiBaseUrl, operatorSecret);
      setRedemptions(response.redemptions ?? []);
      setRewardError('');
    } catch (error) {
      setRewardError(error instanceof Error ? error.message : 'Failed to load redemptions.');
    }
  }

  async function updateRedemption(redemption: TwitchRedemption, status: 'FULFILLED' | 'CANCELED') {
    try {
      await updateTwitchRedemptionStatus(redemption.reward.id || selectedRewardId, redemption.id, status, apiBaseUrl, operatorSecret);
      await loadRedemptions(redemption.reward.id || selectedRewardId);
    } catch (error) {
      setRewardError(error instanceof Error ? error.message : 'Failed to update redemption.');
    }
  }

  async function createReward() {
    if (!rewardDraft.title.trim()) {
      setRewardError('Reward title is required.');
      return;
    }
    if (rewardDraft.cost < 1) {
      setRewardError('Reward cost must be at least 1.');
      return;
    }
    try {
      await createTwitchReward(rewardDraft, apiBaseUrl, operatorSecret);
      setRewardDraft({ title: '', prompt: '', cost: 100, enabled: false, userInputRequired: false });
      await loadRewards();
    } catch (error) {
      setRewardError(error instanceof Error ? error.message : 'Failed to create reward.');
    }
  }

  async function deleteReward() {
    if (!selectedRewardId || !deleteRewardConfirmed) return;
    try {
      await deleteTwitchReward(selectedRewardId, apiBaseUrl, operatorSecret);
      setRedemptions([]);
      await loadRewards();
    } catch (error) {
      setRewardError(error instanceof Error ? error.message : 'Failed to delete reward.');
    }
  }

  async function updateSelectedRewardLifecycle(patch: { enabled?: boolean; paused?: boolean }) {
    if (!selectedReward) return;
    try {
      await updateTwitchReward(selectedReward.id, patch, apiBaseUrl, operatorSecret);
      await loadRewards(selectedReward.id);
    } catch (error) {
      setRewardError(error instanceof Error ? error.message : 'Failed to update reward lifecycle.');
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
                    <input className="hud-input" type="text" value={rule.alias} onChange={(event) => setRules((current) => updateTwitchAliasRule(current, index, { alias: event.target.value }))} placeholder="panic" />
                  </label>
                  <label className="grid gap-1 text-xs text-zinc-300">
                    <span className="hud-kicker">Command</span>
                    <select className="hud-input" value={rule.command} onChange={(event) => setRules((current) => updateTwitchAliasRule(current, index, { command: event.target.value as ChatAliasRule['command'] }))}>
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
                      <input type="checkbox" checked={rule.enabled} onChange={(event) => setRules((current) => updateTwitchAliasRule(current, index, { enabled: event.target.checked }))} />
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
                      <input className="hud-input" type="text" value={rule.rewardTitle} onChange={(event) => setRules((current) => updateTwitchRewardRule(current, index, { rewardTitle: event.target.value }))} placeholder="Hydrate break" />
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300">
                      <span className="hud-kicker">Action</span>
                      <select className="hud-input" value={rule.action} onChange={(event) => setRules((current) => updateTwitchRewardRule(current, index, { action: event.target.value as ChannelPointRule['action'] }))}>
                        {rewardActions.map((action) => (
                          <option key={action} value={action}>
                            {action}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300">
                      <span className="hud-kicker">Enabled</span>
                      <input type="checkbox" checked={rule.enabled} onChange={(event) => setRules((current) => updateTwitchRewardRule(current, index, { enabled: event.target.checked }))} />
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300">
                      <span className="hud-kicker">Cooldown seconds</span>
                      <input className="hud-input" type="number" min={0} max={86400} value={rule.cooldownSeconds ?? 0} onChange={(event) => setRules((current) => updateTwitchRewardRule(current, index, { cooldownSeconds: Number(event.target.value) }))} />
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300 md:col-span-2">
                      <span className="hud-kicker">Title override</span>
                      <input className="hud-input" type="text" value={rule.title ?? ''} onChange={(event) => setRules((current) => updateTwitchRewardRule(current, index, { title: event.target.value }))} placeholder="Optional alert title" />
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300 md:col-span-2">
                      <span className="hud-kicker">Message override</span>
                      <textarea className="hud-input min-h-20 font-mono text-xs" value={rule.message ?? ''} onChange={(event) => setRules((current) => updateTwitchRewardRule(current, index, { message: event.target.value }))} placeholder="Optional alert message" />
                    </label>
                    <label className="grid gap-1 text-xs text-zinc-300 md:col-span-2">
                      <span className="hud-kicker">Fulfillment note</span>
                      <input className="hud-input" type="text" value={rule.fulfillmentNote ?? ''} onChange={(event) => setRules((current) => updateTwitchRewardRule(current, index, { fulfillmentNote: event.target.value }))} placeholder="Optional operator note" />
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

      <div className="mt-3 hud-panel hud-corners hud-panel--flat hud-compact-card">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <p className="hud-kicker">Reward queue</p>
            <p className="mt-1 text-xs text-zinc-400">Load manageable rewards, create simple rewards, and fulfill/cancel unfulfilled redemptions. Delete requires confirmation and can fulfill pending redemptions on Twitch.</p>
          </div>
          <button className="hud-chip" type="button" onClick={() => loadRewards()}>Load rewards</button>
        </div>
        {rewardError ? <p className="mt-2 border border-amber-400/40 bg-black px-2 py-1 text-xs text-amber-100">{rewardError}</p> : null}
        <div className="mt-3 grid gap-2 rounded-2xl border border-emerald-400/20 bg-emerald-400/5 p-3 md:grid-cols-[1fr_8rem_auto_auto] md:items-end">
          <label className="grid gap-1 text-xs text-zinc-300">
            <span className="hud-kicker">New reward title</span>
            <input className="hud-input" type="text" maxLength={45} value={rewardDraft.title} onChange={(event) => setRewardDraft((current) => ({ ...current, title: event.target.value }))} placeholder="Hydrate break" />
          </label>
          <label className="grid gap-1 text-xs text-zinc-300">
            <span className="hud-kicker">Cost</span>
            <input className="hud-input" type="number" min={1} value={rewardDraft.cost} onChange={(event) => setRewardDraft((current) => ({ ...current, cost: Number(event.target.value) }))} />
          </label>
          <label className="flex items-center gap-2 text-xs text-zinc-300">
            <input type="checkbox" checked={rewardDraft.enabled} onChange={(event) => setRewardDraft((current) => ({ ...current, enabled: event.target.checked }))} />
            Enabled
          </label>
          <label className="flex items-center gap-2 text-xs text-zinc-300">
            <input type="checkbox" checked={rewardDraft.userInputRequired} onChange={(event) => setRewardDraft((current) => ({ ...current, userInputRequired: event.target.checked }))} />
            Require input
          </label>
          <label className="grid gap-1 text-xs text-zinc-300 md:col-span-3">
            <span className="hud-kicker">Prompt</span>
            <input className="hud-input" type="text" maxLength={200} value={rewardDraft.prompt} onChange={(event) => setRewardDraft((current) => ({ ...current, prompt: event.target.value }))} placeholder="Optional viewer prompt" />
          </label>
          <button className="hud-chip self-end" type="button" onClick={createReward} disabled={!rewardDraft.title.trim()}>
            Create reward
          </button>
        </div>
        {rewards.length > 0 ? (
          <div className="mt-3 flex flex-wrap items-end gap-2">
            <label className="grid min-w-64 gap-1 text-xs text-zinc-300"><span className="hud-kicker">Reward</span><select className="hud-input" value={selectedRewardId} onChange={(event) => { setSelectedRewardId(event.target.value); setDeleteRewardConfirmed(false); }}>{rewards.map((reward) => <option key={reward.id} value={reward.id}>{reward.title} · {reward.cost}</option>)}</select></label>
            <button className="hud-chip" type="button" onClick={() => loadRedemptions()}>Load unfulfilled</button>
          </div>
        ) : null}
        {selectedReward ? (
          <div className="mt-3 rounded-2xl border border-cyan-400/20 bg-cyan-400/5 p-3 text-xs text-zinc-200">
            <p className="hud-kicker">Selected reward context</p>
            <p className="mt-1 font-semibold text-cyan-100">
              {selectedReward.title} · {selectedReward.cost}
            </p>
            <div className="mt-2 flex flex-wrap gap-2">
              <span className="hud-chip">{selectedReward.is_enabled === false ? 'Disabled' : 'Enabled'}</span>
              <span className="hud-chip">{selectedReward.is_paused ? 'Paused' : 'Not paused'}</span>
              {selectedReward.is_in_stock === false ? <span className="hud-chip">Out of stock</span> : null}
            </div>
            {selectedRewardRule?.fulfillmentNote ? <p className="mt-1 text-zinc-200">Fulfillment note: {selectedRewardRule.fulfillmentNote}</p> : <p className="mt-1 text-zinc-400">No saved channel-point rule match.</p>}
            <p className="mt-1 text-zinc-300">Pause or disable first when you need a reversible change. Delete remains destructive and may fulfill pending redemptions on Twitch.</p>
            <div className="mt-2 flex flex-wrap gap-2">
              <button className="hud-chip" type="button" onClick={() => updateSelectedRewardLifecycle({ paused: !selectedReward.is_paused })}>
                {selectedReward.is_paused ? 'Resume reward' : 'Pause reward'}
              </button>
              <button className="hud-chip" type="button" onClick={() => updateSelectedRewardLifecycle({ enabled: selectedReward.is_enabled === false })}>
                {selectedReward.is_enabled === false ? 'Enable reward' : 'Disable reward'}
              </button>
            </div>
          </div>
        ) : null}
        {rewards.length > 0 ? (
          <div className="mt-3 flex flex-wrap items-center gap-2 border-t border-zinc-800 pt-3">
            <p className="text-xs text-amber-100">Delete is last resort. Pause or disable first when the reward should be reversible.</p>
            <label className="flex items-center gap-2 text-xs text-amber-100">
              <input type="checkbox" checked={deleteRewardConfirmed} onChange={(event) => setDeleteRewardConfirmed(event.target.checked)} />
              Confirm delete
            </label>
            <button className="hud-chip" type="button" onClick={deleteReward} disabled={!deleteRewardConfirmed || !selectedRewardId}>Delete selected</button>
          </div>
        ) : null}
        <div className="mt-3 grid gap-2">
          {redemptions.length === 0 ? <p className="text-xs text-zinc-400">No unfulfilled redemptions loaded.</p> : null}
          {redemptionsWithRules.map(({ redemption, matchedRule }) => (
            <article key={redemption.id} className="rounded-2xl border border-fuchsia-400/20 bg-fuchsia-400/5 p-3 text-xs">
              <p className="font-semibold text-fuchsia-100">
                {redemption.user_name || redemption.user_login} · {redemption.reward.title}
              </p>
              {matchedRule?.fulfillmentNote ? <p className="mt-1 text-zinc-200">Fulfillment note: {matchedRule.fulfillmentNote}</p> : null}
              <p className="mt-1 text-zinc-300">{redemption.user_input || 'No input'}</p>
              <p className="mt-1 text-zinc-500">{redemption.status} · {redemption.redeemed_at}</p>
              <div className="mt-2 flex gap-2">
                <button className="hud-chip" type="button" onClick={() => updateRedemption(redemption, 'FULFILLED')}>
                  Fulfill
                </button>
                <button className="hud-chip" type="button" onClick={() => updateRedemption(redemption, 'CANCELED')}>
                  Cancel
                </button>
              </div>
            </article>
          ))}
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
