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
import { Check, Empty, Field, Notice, Panel, SubPanel, Tag } from '../../ui';

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
  globalCooldownSeconds: number;
  maxPerStream: number;
  maxPerUserPerStream: number;
};

type RewardEditDraft = {
  prompt: string;
  userInputRequired: boolean;
  globalCooldownSeconds: number;
  maxPerStream: number;
  maxPerUserPerStream: number;
};

const emptyRewardDraft: RewardDraft = {
  title: '',
  prompt: '',
  cost: 100,
  enabled: false,
  userInputRequired: false,
  globalCooldownSeconds: 0,
  maxPerStream: 0,
  maxPerUserPerStream: 0,
};

function rewardLimitValue(enabled?: boolean, value?: number) {
  return enabled ? value ?? 0 : 0;
}

function rewardEditDraftFromReward(reward?: TwitchReward): RewardEditDraft {
  return {
    prompt: reward?.prompt ?? '',
    userInputRequired: reward?.is_user_input_required ?? false,
    globalCooldownSeconds: rewardLimitValue(reward?.is_global_cooldown_enabled, reward?.global_cooldown_seconds),
    maxPerStream: rewardLimitValue(reward?.is_max_per_stream_enabled, reward?.max_per_stream),
    maxPerUserPerStream: rewardLimitValue(reward?.is_max_per_user_per_stream_enabled, reward?.max_per_user_per_stream),
  };
}

function validateRewardLimits(values: { globalCooldownSeconds: number; maxPerStream: number; maxPerUserPerStream: number }) {
  if (!Number.isFinite(values.globalCooldownSeconds)) return 'Global cooldown seconds must be a number.';
  if (!Number.isFinite(values.maxPerStream)) return 'Max per stream must be a number.';
  if (!Number.isFinite(values.maxPerUserPerStream)) return 'Max per user/stream must be a number.';
  if (!Number.isInteger(values.globalCooldownSeconds)) return 'Global cooldown seconds must be a whole number.';
  if (!Number.isInteger(values.maxPerStream)) return 'Max per stream must be a whole number.';
  if (!Number.isInteger(values.maxPerUserPerStream)) return 'Max per user/stream must be a whole number.';
  if (values.globalCooldownSeconds < 0) return 'Global cooldown seconds must be zero or greater.';
  if (values.globalCooldownSeconds > 0 && values.globalCooldownSeconds < 60) return 'Global cooldown seconds must be 0 or at least 60 so Twitch can display it.';
  if (values.maxPerStream < 0) return 'Max per stream must be zero or greater.';
  if (values.maxPerUserPerStream < 0) return 'Max per user/stream must be zero or greater.';
  return '';
}

function rewardNumberInput(value: string) {
  if (value.trim() === '') return 0;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function buildCreateRewardPayload(draft: RewardDraft) {
  return {
    title: draft.title,
    cost: draft.cost,
    enabled: draft.enabled,
    userInputRequired: draft.userInputRequired,
    ...(draft.prompt.trim() ? { prompt: draft.prompt } : {}),
    ...(draft.globalCooldownSeconds > 0 ? { globalCooldownSeconds: draft.globalCooldownSeconds } : {}),
    ...(draft.maxPerStream > 0 ? { maxPerStream: draft.maxPerStream } : {}),
    ...(draft.maxPerUserPerStream > 0 ? { maxPerUserPerStream: draft.maxPerUserPerStream } : {}),
  };
}

export function TwitchRewardQueueManager({ apiBaseUrl, operatorSecret, channelPointRules }: TwitchRewardQueueManagerProps) {
  const [rewards, setRewards] = useState<TwitchReward[]>([]);
  const [redemptions, setRedemptions] = useState<TwitchRedemption[]>([]);
  const [selectedRewardId, setSelectedRewardId] = useState('');
  const [rewardError, setRewardError] = useState('');
  const [rewardDraft, setRewardDraft] = useState<RewardDraft>(emptyRewardDraft);
  const [rewardEditDraft, setRewardEditDraft] = useState<RewardEditDraft>(rewardEditDraftFromReward());
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
      const nextSelectedReward = loadedRewards.find((reward) => reward.id === nextSelectedRewardId);
      setRewards(loadedRewards);
      setSelectedRewardId(nextSelectedRewardId);
      setRewardEditDraft(rewardEditDraftFromReward(nextSelectedReward));
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
    if (!Number.isFinite(rewardDraft.cost)) {
      setRewardError('Reward cost must be a number.');
      return;
    }
    if (!Number.isInteger(rewardDraft.cost)) {
      setRewardError('Reward cost must be a whole number.');
      return;
    }
    if (rewardDraft.cost < 1) {
      setRewardError('Reward cost must be at least 1.');
      return;
    }
    const limitError = validateRewardLimits(rewardDraft);
    if (limitError) {
      setRewardError(limitError);
      return;
    }
    try {
      await createTwitchReward(buildCreateRewardPayload(rewardDraft), apiBaseUrl, operatorSecret);
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

  async function updateSelectedRewardAdvancedFields() {
    if (!selectedReward) return;
    const limitError = validateRewardLimits(rewardEditDraft);
    if (limitError) {
      setRewardError(limitError);
      return;
    }
    const currentDraft = rewardEditDraftFromReward(selectedReward);
    const patch = {
      ...(rewardEditDraft.prompt !== currentDraft.prompt ? { prompt: rewardEditDraft.prompt } : {}),
      ...(rewardEditDraft.userInputRequired !== currentDraft.userInputRequired ? { userInputRequired: rewardEditDraft.userInputRequired } : {}),
      ...(rewardEditDraft.globalCooldownSeconds !== currentDraft.globalCooldownSeconds ? { globalCooldownSeconds: rewardEditDraft.globalCooldownSeconds } : {}),
      ...(rewardEditDraft.maxPerStream !== currentDraft.maxPerStream ? { maxPerStream: rewardEditDraft.maxPerStream } : {}),
      ...(rewardEditDraft.maxPerUserPerStream !== currentDraft.maxPerUserPerStream ? { maxPerUserPerStream: rewardEditDraft.maxPerUserPerStream } : {}),
    };
    if (Object.keys(patch).length === 0) {
      setRewardError('No reward field changes to save.');
      return;
    }
    try {
      await updateTwitchReward(selectedReward.id, patch, apiBaseUrl, operatorSecret);
      await loadRewards(selectedReward.id);
    } catch (error) {
      setRewardError(error instanceof Error ? error.message : 'Failed to update reward fields.');
    }
  }

  return (
    <Panel
      id="reward-queue"
      kicker="Twitch"
      title="Reward queue"
      description="Load manageable rewards, create simple rewards, and fulfill or cancel unfulfilled redemptions. Delete requires confirmation and can fulfill pending redemptions on Twitch."
      actions={
        <button className="desk-btn" type="button" onClick={() => void loadRewards()}>
          Load rewards
        </button>
      }
    >
      {rewardError ? (
        <Notice tone="bad" role="alert">
          {rewardError}
        </Notice>
      ) : null}

      <SubPanel title="New reward">
        <div className="grid gap-3 md:grid-cols-4 md:items-end">
          <Field label="New reward title" className="md:col-span-2">
            <input className="desk-input" type="text" maxLength={45} value={rewardDraft.title} onChange={(event) => setRewardDraft((current) => ({ ...current, title: event.target.value }))} placeholder="Hydrate break" />
          </Field>
          <Field label="Cost">
            <input
              className="desk-input"
              type="number"
              min={1}
              step={1}
              value={rewardDraft.cost}
              onChange={(event) => {
                const nextValue = rewardNumberInput(event.target.value);
                if (nextValue !== undefined) setRewardDraft((current) => ({ ...current, cost: nextValue }));
              }}
            />
          </Field>
          <div className="flex min-h-8 flex-wrap items-center gap-3">
            <Check label="Enabled" checked={rewardDraft.enabled} onChange={(checked) => setRewardDraft((current) => ({ ...current, enabled: checked }))} />
            <Check label="Require input" checked={rewardDraft.userInputRequired} onChange={(checked) => setRewardDraft((current) => ({ ...current, userInputRequired: checked }))} />
          </div>
          <Field label="Prompt" className="md:col-span-4">
            <input className="desk-input" type="text" maxLength={200} value={rewardDraft.prompt} onChange={(event) => setRewardDraft((current) => ({ ...current, prompt: event.target.value }))} placeholder="Optional viewer prompt" />
          </Field>
          <Field label="Global cooldown seconds">
            <input
              className="desk-input"
              type="number"
              min={0}
              step={1}
              value={rewardDraft.globalCooldownSeconds}
              onChange={(event) => {
                const nextValue = rewardNumberInput(event.target.value);
                if (nextValue !== undefined) setRewardDraft((current) => ({ ...current, globalCooldownSeconds: nextValue }));
              }}
            />
          </Field>
          <Field label="Max per stream">
            <input
              className="desk-input"
              type="number"
              min={0}
              step={1}
              value={rewardDraft.maxPerStream}
              onChange={(event) => {
                const nextValue = rewardNumberInput(event.target.value);
                if (nextValue !== undefined) setRewardDraft((current) => ({ ...current, maxPerStream: nextValue }));
              }}
            />
          </Field>
          <Field label="Max per user/stream">
            <input
              className="desk-input"
              type="number"
              min={0}
              step={1}
              value={rewardDraft.maxPerUserPerStream}
              onChange={(event) => {
                const nextValue = rewardNumberInput(event.target.value);
                if (nextValue !== undefined) setRewardDraft((current) => ({ ...current, maxPerUserPerStream: nextValue }));
              }}
            />
          </Field>
          <button className="desk-btn desk-btn--primary" type="button" onClick={createReward} disabled={!rewardDraft.title.trim()}>
            Create reward
          </button>
          <p className="desk-field__hint md:col-span-4">Use zero to disable optional limits. Global cooldown must be 0 or at least 60 seconds so Twitch can display it.</p>
        </div>
      </SubPanel>

      {rewards.length > 0 ? (
        <div className="flex flex-wrap items-end gap-2">
          <Field label="Reward" className="min-w-64">
            <select
              className="desk-input"
              value={selectedRewardId}
              onChange={(event) => {
                const nextRewardId = event.target.value;
                const nextReward = rewards.find((reward) => reward.id === nextRewardId);
                setSelectedRewardId(nextRewardId);
                setRewardEditDraft(rewardEditDraftFromReward(nextReward));
                setDeleteRewardConfirmed(false);
              }}
            >
              {rewards.map((reward) => (
                <option key={reward.id} value={reward.id}>
                  {reward.title} · {reward.cost}
                </option>
              ))}
            </select>
          </Field>
          <button className="desk-btn" type="button" onClick={() => void loadRedemptions()}>
            Load unfulfilled
          </button>
        </div>
      ) : null}

      {selectedReward ? (
        <SubPanel title="Selected reward">
          <div className="desk-row__line">
            <p className="desk-row__title">
              {selectedReward.title} · <span className="mono">{selectedReward.cost}</span>
            </p>
            <div className="flex flex-wrap gap-2">
              <Tag tone={selectedReward.is_enabled === false ? 'warn' : 'ok'}>{selectedReward.is_enabled === false ? 'Disabled' : 'Enabled'}</Tag>
              <Tag tone={selectedReward.is_paused ? 'warn' : 'neutral'}>{selectedReward.is_paused ? 'Paused' : 'Not paused'}</Tag>
              {selectedReward.is_in_stock === false ? <Tag tone="bad">Out of stock</Tag> : null}
            </div>
          </div>
          {selectedRewardRule?.fulfillmentNote ? <p className="desk-row__text">Fulfillment note: {selectedRewardRule.fulfillmentNote}</p> : <Empty>No saved channel-point rule match.</Empty>}
          <p className="desk-row__text">Pause or disable first when you need a reversible change. Delete is permanent and may fulfill pending redemptions on Twitch.</p>
          <div className="flex flex-wrap gap-2">
            <button className="desk-btn desk-btn--small" type="button" onClick={() => void updateSelectedRewardLifecycle({ paused: !selectedReward.is_paused })}>
              {selectedReward.is_paused ? 'Resume reward' : 'Pause reward'}
            </button>
            <button className="desk-btn desk-btn--small" type="button" onClick={() => void updateSelectedRewardLifecycle({ enabled: selectedReward.is_enabled === false })}>
              {selectedReward.is_enabled === false ? 'Enable reward' : 'Disable reward'}
            </button>
          </div>
          <div className="grid gap-3 md:grid-cols-3 md:items-end">
            <Field label="Prompt" className="md:col-span-3">
              <input className="desk-input" type="text" maxLength={200} value={rewardEditDraft.prompt} onChange={(event) => setRewardEditDraft((current) => ({ ...current, prompt: event.target.value }))} />
            </Field>
            <Check
              label="Require viewer input"
              className="md:col-span-3"
              checked={rewardEditDraft.userInputRequired}
              onChange={(checked) => setRewardEditDraft((current) => ({ ...current, userInputRequired: checked }))}
            />
            <Field label="Global cooldown seconds">
            <input
              className="desk-input"
              type="number"
              min={0}
              step={1}
              value={rewardEditDraft.globalCooldownSeconds}
              onChange={(event) => {
                const nextValue = rewardNumberInput(event.target.value);
                if (nextValue !== undefined) setRewardEditDraft((current) => ({ ...current, globalCooldownSeconds: nextValue }));
              }}
            />
          </Field>
            <Field label="Max per stream">
            <input
              className="desk-input"
              type="number"
              min={0}
              step={1}
              value={rewardEditDraft.maxPerStream}
              onChange={(event) => {
                const nextValue = rewardNumberInput(event.target.value);
                if (nextValue !== undefined) setRewardEditDraft((current) => ({ ...current, maxPerStream: nextValue }));
              }}
            />
          </Field>
            <Field label="Max per user/stream">
            <input
              className="desk-input"
              type="number"
              min={0}
              step={1}
              value={rewardEditDraft.maxPerUserPerStream}
              onChange={(event) => {
                const nextValue = rewardNumberInput(event.target.value);
                if (nextValue !== undefined) setRewardEditDraft((current) => ({ ...current, maxPerUserPerStream: nextValue }));
              }}
            />
          </Field>
            <p className="desk-field__hint md:col-span-2">Use zero to disable optional limits. Global cooldown must be 0 or at least 60 seconds so Twitch can display it.</p>
            <button className="desk-btn desk-btn--primary" type="button" onClick={() => void updateSelectedRewardAdvancedFields()}>
              Save reward fields
            </button>
          </div>
        </SubPanel>
      ) : null}

      {rewards.length > 0 ? (
        <div className="grid gap-2">
          <Notice tone="warn">Delete is a last resort. Pause or disable first when the change should be reversible.</Notice>
          <div className="flex flex-wrap items-center gap-3">
            <Check label="Confirm delete" checked={deleteRewardConfirmed} onChange={setDeleteRewardConfirmed} />
            <button className="desk-btn desk-btn--danger" type="button" onClick={() => void deleteReward()} disabled={!deleteRewardConfirmed || !selectedRewardId}>
              Delete selected
            </button>
          </div>
        </div>
      ) : null}

      {redemptions.length === 0 ? (
        <Empty>No unfulfilled redemptions loaded.</Empty>
      ) : (
        <ul className="desk-rows">
          {redemptionsWithRules.map(({ redemption, matchedRule }) => (
            <li key={redemption.id} className="desk-row">
              <div className="desk-row__line">
                <p className="desk-row__title">
                  {redemption.user_name || redemption.user_login} · {redemption.reward.title}
                </p>
                <span className="desk-row__meta mono">
                  {redemption.status} · {redemption.redeemed_at}
                </span>
              </div>
              {matchedRule?.fulfillmentNote ? <p className="desk-row__text">Fulfillment note: {matchedRule.fulfillmentNote}</p> : null}
              <p className="desk-row__text">{redemption.user_input || 'No input'}</p>
              <div className="mt-1 flex gap-2">
                <button className="desk-btn desk-btn--small" type="button" onClick={() => void updateRedemption(redemption, 'FULFILLED')}>
                  Fulfill
                </button>
                <button className="desk-btn desk-btn--small desk-btn--danger" type="button" onClick={() => void updateRedemption(redemption, 'CANCELED')}>
                  Cancel
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}
