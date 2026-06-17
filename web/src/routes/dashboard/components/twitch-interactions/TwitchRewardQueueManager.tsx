import { useMemo, useState } from 'react';
import {
  createTwitchReward,
  deleteTwitchReward,
  fetchTwitchRewardRedemptions,
  fetchTwitchRewards,
  updateTwitchRedemptionStatus,
  updateTwitchReward,
  type ChannelPointRule,
  type TwitchRedemption,
  type TwitchReward,
} from '../../../../lib/api';
import { findMatchingChannelPointRule } from '../../../../lib/twitchInteractionRules';

type TwitchRewardQueueManagerProps = {
  apiBaseUrl?: string;
  operatorSecret: string;
  channelPointRules: ChannelPointRule[];
};

type RewardDraft = {
  title: string;
  prompt: string;
  cost: number;
  enabled: boolean;
  userInputRequired: boolean;
};

const emptyRewardDraft: RewardDraft = { title: '', prompt: '', cost: 100, enabled: false, userInputRequired: false };

export function TwitchRewardQueueManager({ apiBaseUrl, operatorSecret, channelPointRules }: TwitchRewardQueueManagerProps) {
  const [rewards, setRewards] = useState<TwitchReward[]>([]);
  const [redemptions, setRedemptions] = useState<TwitchRedemption[]>([]);
  const [selectedRewardId, setSelectedRewardId] = useState('');
  const [rewardError, setRewardError] = useState('');
  const [rewardDraft, setRewardDraft] = useState<RewardDraft>(emptyRewardDraft);
  const [deleteRewardConfirmed, setDeleteRewardConfirmed] = useState(false);

  const selectedReward = useMemo(() => rewards.find((reward) => reward.id === selectedRewardId), [rewards, selectedRewardId]);
  const selectedRewardRule = useMemo(
    () => (selectedReward ? findMatchingChannelPointRule(channelPointRules, selectedReward.title) : undefined),
    [channelPointRules, selectedReward],
  );
  const redemptionsWithRules = useMemo(
    () => redemptions.map((redemption) => ({ redemption, matchedRule: findMatchingChannelPointRule(channelPointRules, redemption.reward.title) })),
    [channelPointRules, redemptions],
  );

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
      setRewardDraft(emptyRewardDraft);
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
    <div className="mt-3 hud-panel hud-corners hud-panel--flat hud-compact-card">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <p className="hud-kicker">Reward queue</p>
          <p className="mt-1 text-xs text-zinc-400">Load manageable rewards, create simple rewards, and fulfill/cancel unfulfilled redemptions. Delete requires confirmation and can fulfill pending redemptions on Twitch.</p>
        </div>
        <button className="hud-chip" type="button" onClick={() => void loadRewards()}>
          Load rewards
        </button>
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
          <label className="grid min-w-64 gap-1 text-xs text-zinc-300">
            <span className="hud-kicker">Reward</span>
            <select
              className="hud-input"
              value={selectedRewardId}
              onChange={(event) => {
                setSelectedRewardId(event.target.value);
                setDeleteRewardConfirmed(false);
              }}
            >
              {rewards.map((reward) => (
                <option key={reward.id} value={reward.id}>
                  {reward.title} · {reward.cost}
                </option>
              ))}
            </select>
          </label>
          <button className="hud-chip" type="button" onClick={() => void loadRedemptions()}>
            Load unfulfilled
          </button>
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
            <button className="hud-chip" type="button" onClick={() => void updateSelectedRewardLifecycle({ paused: !selectedReward.is_paused })}>
              {selectedReward.is_paused ? 'Resume reward' : 'Pause reward'}
            </button>
            <button className="hud-chip" type="button" onClick={() => void updateSelectedRewardLifecycle({ enabled: selectedReward.is_enabled === false })}>
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
          <button className="hud-chip" type="button" onClick={() => void deleteReward()} disabled={!deleteRewardConfirmed || !selectedRewardId}>
            Delete selected
          </button>
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
            <p className="mt-1 text-zinc-500">
              {redemption.status} · {redemption.redeemed_at}
            </p>
            <div className="mt-2 flex gap-2">
              <button className="hud-chip" type="button" onClick={() => void updateRedemption(redemption, 'FULFILLED')}>
                Fulfill
              </button>
              <button className="hud-chip" type="button" onClick={() => void updateRedemption(redemption, 'CANCELED')}>
                Cancel
              </button>
            </div>
          </article>
        ))}
      </div>
    </div>
  );
}
