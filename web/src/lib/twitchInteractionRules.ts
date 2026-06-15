import type { ChatAliasRule, ChannelPointRule, TwitchInteractionRules } from './api';

export const aliasCommands = ['alert', 'clear', 'brb', 'back'] as const;
export const rewardActions = ['alert', 'brb', 'clear', 'ignore'] as const;

export function createEmptyTwitchInteractionRules(): TwitchInteractionRules {
  return { chatAliases: [], channelPointRules: [] };
}

export function cloneTwitchInteractionRules(rules: TwitchInteractionRules): TwitchInteractionRules {
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

export function normalizeImportedTwitchInteractionRules(value: unknown): TwitchInteractionRules {
  if (!isRules(value)) {
    throw new Error('invalid rules JSON');
  }

  const chatAliases = value.chatAliases
    .filter(
      (entry): entry is ChatAliasRule =>
        typeof entry === 'object' &&
        entry !== null &&
        typeof entry.alias === 'string' &&
        typeof entry.command === 'string' &&
        typeof entry.enabled === 'boolean',
    )
    .map((entry) => ({
      alias: entry.alias,
      command: aliasCommands.includes(entry.command as (typeof aliasCommands)[number]) ? (entry.command as ChatAliasRule['command']) : 'alert',
      enabled: entry.enabled,
    }));

  const channelPointRules = value.channelPointRules
    .filter(
      (entry): entry is ChannelPointRule =>
        typeof entry === 'object' &&
        entry !== null &&
        typeof entry.rewardTitle === 'string' &&
        typeof entry.action === 'string' &&
        typeof entry.enabled === 'boolean',
    )
    .map((entry) => {
      const rule: ChannelPointRule = {
        rewardTitle: entry.rewardTitle,
        action: rewardActions.includes(entry.action as (typeof rewardActions)[number]) ? (entry.action as ChannelPointRule['action']) : 'ignore',
        title: typeof entry.title === 'string' ? entry.title : undefined,
        message: typeof entry.message === 'string' ? entry.message : undefined,
        enabled: entry.enabled,
      };
      if (typeof entry.cooldownSeconds === 'number') {
        rule.cooldownSeconds = Math.max(0, Math.min(86400, entry.cooldownSeconds));
      }
      if (typeof entry.fulfillmentNote === 'string') {
        rule.fulfillmentNote = entry.fulfillmentNote;
      }
      return rule;
    });

  return { chatAliases, channelPointRules };
}

export function updateTwitchAliasRule(rules: TwitchInteractionRules, index: number, patch: Partial<ChatAliasRule>) {
  const next = cloneTwitchInteractionRules(rules);
  next.chatAliases[index] = { ...next.chatAliases[index], ...patch };
  return next;
}

export function updateTwitchRewardRule(rules: TwitchInteractionRules, index: number, patch: Partial<ChannelPointRule>) {
  const next = cloneTwitchInteractionRules(rules);
  next.channelPointRules[index] = { ...next.channelPointRules[index], ...patch };
  return next;
}
