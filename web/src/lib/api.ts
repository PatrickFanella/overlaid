import { requestJSON } from './apiRequest';
import { getOverlayApiBaseUrl } from './eventStream';
import type { DashboardConfig } from './dashboardConfig';
import type { OverlayEventType, OverlaySeverity } from './events';

export type TwitchStatus = {
  configured: boolean;
  authenticated: boolean;
  channelTarget: string;
  loginUrl: string;
  scopes: string[];
  streamerLogin?: string;
  streamerUserId?: string;
  tokenExpiresAt?: string;
  authError?: string;
  lastAuthError?: string;
  missingScopes?: string[];
  eventSub?: {
    enabled: boolean;
    connected: boolean;
    lastConnectedAt?: string;
    lastDisconnectedAt?: string;
    lastEventAt?: string;
    lastError?: string;
    reconnectAttempts: number;
    subscriptionsMade: number;
    subscriptionsPlan: number;
  };
};

export type BotStatus = {
 commandStorage?: string;
  available: boolean;
  loadError?: string;
  lastAuditError?: string;
  commandCount: number;
  timerCount: number;
  counterCount: number;
  quoteCount: number;
  auditCount: number;
};

export type BotAuditEvent = {
  id: string;
  createdAt: string;
  source: string;
  actorId?: string;
  actorLogin?: string;
  actorDisplayName?: string;
  actorRole?: string;
  trigger?: string;
  command?: string;
  action?: string;
  result: string;
  reason?: string;
  eventId?: string;
  streamSessionId?: string;
  data?: Record<string, unknown>;
};

export type BotAuditResponse = {
  events: BotAuditEvent[];
  loadError?: string;
};

export type BotTimer = {
  id: string;
  name: string;
  enabled: boolean;
  intervalSeconds: number;
  response: string;
  action: 'ticker' | 'alert' | string;
  createdAt: string;
  updatedAt: string;
  lastRunAt?: string;
};

export type BotTimersResponse = {
  timers: BotTimer[];
  loadError?: string;
};

export type BotCommand = {
  id: string;
  name: string;
  enabled: boolean;
  response: string;
  action: 'ticker' | 'alert' | string;
  responseMode: 'overlay' | 'chat' | 'both' | string;
  roleRequired: 'viewer' | 'moderator' | 'broadcaster' | string;
  cooldownSeconds: number;
  createdAt: string;
  updatedAt: string;
};

export type BotCommandsResponse = {
  commands: BotCommand[];
  loadError?: string;
};

export type BotCounter = { id: string; name: string; value: number; createdAt: string; updatedAt: string };
export type BotQuote = { id: string; text: string; author?: string; createdAt: string; updatedAt: string };
export type BotCountersResponse = { counters: BotCounter[]; loadError?: string };
export type BotQuotesResponse = { quotes: BotQuote[]; loadError?: string };
export type BotAPIVariable = { id: string; name: string; enabled: boolean; url: string; responseSelector?: string; fallback?: string; cacheTtlSeconds: number; timeoutMs: number; lastStatus?: string; lastError?: string; lastCheckedAt?: string; createdAt: string; updatedAt: string };
export type BotAPIVariablesResponse = { variables: BotAPIVariable[]; loadError?: string };
export type BotCounterPayload = { id?: string; name: string; value: number };
export type BotQuotePayload = { id?: string; text: string; author?: string };
export type BotAPIVariablePayload = { id?: string; name: string; enabled: boolean; url: string; responseSelector?: string; fallback?: string; cacheTtlSeconds: number; timeoutMs: number };
export type BotModerationRule = { id: string; name: string; enabled: boolean; enforce?: boolean; exemptRole?: string; timeoutSeconds?: number; pattern: string; matchTarget: 'message' | 'username' | 'both' | string; caseSensitive: boolean; regex: boolean; action: 'flag' | 'delete' | 'timeout' | 'ban' | string; reason?: string; source?: string; expiresAt?: string; createdAt: string; updatedAt: string };
export type BotModerationRulesResponse = { rules: BotModerationRule[]; loadError?: string };
export type BotModerationRulePayload = { id?: string; name: string; enabled: boolean; enforce?: boolean; exemptRole?: string; timeoutSeconds?: number; pattern: string; matchTarget: 'message' | 'username' | 'both'; caseSensitive: boolean; regex: boolean; action: 'flag' | 'delete' | 'timeout' | 'ban'; reason?: string };
export type BotModerationMatch = { ruleId: string; ruleName: string; pattern: string; matchTarget: string; action: string; reason?: string; skipped?: boolean; note?: string };
export type BotModerationDryRunResponse = { matches: BotModerationMatch[]; loadError?: string };
export type BotModerationActionPayload = { action: 'delete' | 'timeout' | 'ban' | 'unban'; targetUserId?: string; targetUserLogin?: string; messageId?: string; durationSeconds?: number; reason?: string };
export type BotModerationActionResponse = { action: string; result: string; reason?: string };
export type BotNukeDryRunPayload = { pattern: string; regex: boolean; caseSensitive: boolean; lookbackMinutes: number; limit: number; action?: 'delete' | 'timeout'; durationSeconds?: number; confirm?: boolean; radiationMinutes?: number };
export type BotNukeDryRunMatch = { eventId: string; messageId?: string; userId?: string; user: string; message: string; createdAt: string };
export type BotNukeDryRunResponse = { matches: BotNukeDryRunMatch[]; pattern: string; action: string };
export type BotNukeExecuteResponse = { interrupted?: boolean; matches: BotNukeDryRunMatch[]; attempted: number; succeeded: number; failed: number; action: string; radiationRule?: BotModerationRule };
export type BotNukeRollbackResponse = { rule: BotModerationRule };

export type BotCommandPayload = {
  id?: string;
  name: string;
  enabled: boolean;
  response: string;
  action: 'ticker' | 'alert' | 'newsroom_submit' | 'newsroom_capture' | 'newsroom_current' | 'newsroom_question' | 'newsroom_raffle';
  responseMode: 'overlay' | 'chat' | 'both';
  roleRequired: 'viewer' | 'supporter' | 'moderator' | 'broadcaster' | 'operator' | 'system';
  cooldownSeconds: number;
};

export type BotTimerPayload = {
  id?: string;
  name: string;
  enabled: boolean;
  intervalSeconds: number;
  response: string;
  action: 'ticker' | 'alert';
};

export type ChatAliasRule = {
  alias: string;
  command: 'alert' | 'clear' | 'brb' | 'back';
  enabled: boolean;
};

export type ChannelPointRule = {
  rewardId?: string;
  rewardTitle: string;
  action: 'alert' | 'brb' | 'clear' | 'ignore';
  title?: string;
  message?: string;
  cooldownSeconds?: number;
  fulfillmentNote?: string;
  enabled: boolean;
};

export type TwitchInteractionRules = {
  chatAliases: ChatAliasRule[];
  channelPointRules: ChannelPointRule[];
};

export type TwitchInteractionsResponse = {
  rules: TwitchInteractionRules;
  loadError?: string;
};

export type TwitchReward = {
  id: string;
  title: string;
  prompt?: string;
  cost: number;
  is_enabled?: boolean;
  is_paused?: boolean;
  is_in_stock?: boolean;
  is_user_input_required?: boolean;
  is_global_cooldown_enabled?: boolean;
  global_cooldown_seconds?: number;
  is_max_per_stream_enabled?: boolean;
  max_per_stream?: number;
  is_max_per_user_per_stream_enabled?: boolean;
  max_per_user_per_stream?: number;
  manageable?: boolean;
};
export type TwitchRewardsResponse = { rewards: TwitchReward[] };
export type TwitchRewardCreatePayload = {
  title: string;
  prompt?: string;
  cost: number;
  enabled: boolean;
  userInputRequired: boolean;
  globalCooldownSeconds?: number;
  maxPerStream?: number;
  maxPerUserPerStream?: number;
};
export type TwitchRewardUpdatePayload = {
  enabled?: boolean;
  paused?: boolean;
  prompt?: string;
  userInputRequired?: boolean;
  globalCooldownSeconds?: number;
  maxPerStream?: number;
  maxPerUserPerStream?: number;
};
export type TwitchRewardMutationResponse = { reward?: TwitchReward; result: string };
export type TwitchRedemption = { id: string; user_login?: string; user_name?: string; user_input?: string; status: string; redeemed_at: string; reward: { id: string; title: string; cost: number } };
export type TwitchRedemptionsResponse = { redemptions: TwitchRedemption[] };

export type FetchTwitchInteractionsOptions = {
  baseUrl?: string;
  operatorSecret?: string;
};

export type AlertPayload = {
  title: string;
  message: string;
  severity: 'info' | 'success' | 'warning' | 'error';
  data?: Record<string, unknown>;
};

export type EventHistoryItem = {
  id: string;
  type: OverlayEventType;
  source: string;
  title?: string;
  message?: string;
  severity?: OverlaySeverity;
  data?: Record<string, unknown>;
  createdAt: string;
};

export type StreamSession = {
  id: string;
  title: string;
  kind: 'rehearsal' | 'live' | string;
  notes?: string;
  startedAt: string;
  endedAt?: string;
  eventCount: number;
};

export type StreamSessionResponse = {
  current?: StreamSession;
  recent: StreamSession[];
  loadError?: string;
};

export type StreamSessionReadinessSnapshot = {
  id: string;
  sessionId: string;
  phase: string;
  status: ReadinessStatus | string;
  checks: ReadinessCheck[];
  capturedAt: string;
};

export type SessionBotAudit = {
  events: BotAuditEvent[];
  total: number;
  loadError?: string;
};

export type StreamSessionReport = {
  session: StreamSession;
  events: EventHistoryItem[];
  snapshots: StreamSessionReadinessSnapshot[];
  botAudit?: SessionBotAudit;
};

export type EventHistoryResponse = {
  events: EventHistoryItem[];
};

export type DashboardConfigResponse = {
  config?: DashboardConfig;
  updatedAt?: string;
  loadError?: string;
};

export type BRBPayload = {
  active: boolean;
  title: string;
  message: string;
  eta: string;
  items: string[];
  data?: Record<string, unknown>;
};

export type OBSStatus = {
  configured: boolean;
  url: string;
  reachable: boolean;
  authenticated: boolean;
  errorKind?: string;
  obsStudioVersion?: string;
  obsWebSocketVersion?: string;
  rpcVersion?: number;
  error?: string;
};

export type ReadinessStatus = 'ok' | 'degraded';

export type ReadinessCheck = {
  name: string;
  status: ReadinessStatus;
  hint?: string;
};

export type ReadinessReport = {
  status: ReadinessStatus;
  checks: ReadinessCheck[];
};

export type OBSScene = {
  sceneIndex: number;
  sceneName: string;
  sceneUuid?: string;
};

export type OBSInput = {
  inputName: string;
  inputKind?: string;
  inputUuid?: string;
};

export type OBSSceneItem = {
  sceneItemId: number;
  sourceName: string;
  sceneItemEnabled: boolean;
};

export type OBSSceneList = {
  currentProgramSceneName: string;
  scenes: OBSScene[];
};

export type BuildInfo = {
  name: string;
  version: string;
  commit: string;
  builtAt: string;
  goVersion: string;
  os: string;
  arch: string;
};

export function apiUrl(path: string, explicitBaseUrl?: string) {
  return new URL(path, getOverlayApiBaseUrl(explicitBaseUrl)).toString();
}

export async function fetchTwitchStatus(explicitBaseUrl?: string) {
  return requestJSON<TwitchStatus>('/api/auth/twitch/status', { method: 'GET', explicitBaseUrl, errorLabel: 'Twitch status' });
}

export async function fetchBotStatus(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotStatus>('/api/bot/status', {
    method: 'GET',
    explicitBaseUrl,
    operatorSecret,
    errorLabel: 'bot status',
  });
}

export async function fetchBotAudit(limit = 50, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotAuditResponse>(`/api/bot/audit?limit=${encodeURIComponent(String(limit))}`, {
    method: 'GET',
    explicitBaseUrl,
    operatorSecret,
    errorLabel: 'bot audit',
  });
}

export async function fetchBotTimers(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotTimersResponse>('/api/bot/timers', {
    method: 'GET',
    explicitBaseUrl,
    operatorSecret,
    errorLabel: 'bot timers',
  });
}

export async function fetchBotCommands(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotCommandsResponse>('/api/bot/commands', {
    method: 'GET',
    explicitBaseUrl,
    operatorSecret,
    errorLabel: 'bot commands',
  });
}

export async function saveBotCommand(payload: BotCommandPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotCommand>('/api/bot/commands', {
    method: 'POST',
    explicitBaseUrl,
    operatorSecret,
    body: payload,
    errorLabel: 'save bot command',
  });
}

export async function setBotCommandEnabled(id: string, enabled: boolean, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotCommand>(`/api/bot/commands/${encodeURIComponent(id)}`, {
    method: 'PUT',
    explicitBaseUrl,
    operatorSecret,
    body: { enabled },
    errorLabel: 'set bot command enabled',
  });
}

export async function deleteBotCommand(id: string, explicitBaseUrl?: string, operatorSecret?: string) {
  await requestJSON(`/api/bot/commands/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    explicitBaseUrl,
    operatorSecret,
    expectJson: false,
    errorLabel: 'delete bot command',
  });
}

export async function saveBotTimer(payload: BotTimerPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotTimer>('/api/bot/timers', {
    method: 'POST',
    explicitBaseUrl,
    operatorSecret,
    body: payload,
    errorLabel: 'save bot timer',
  });
}

export async function setBotTimerEnabled(id: string, enabled: boolean, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotTimer>(`/api/bot/timers/${encodeURIComponent(id)}`, {
    method: 'PUT',
    explicitBaseUrl,
    operatorSecret,
    body: { enabled },
    errorLabel: 'set bot timer enabled',
  });
}

export async function runBotTimer(id: string, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<{ eventId: string }>(`/api/bot/timers/${encodeURIComponent(id)}/run`, {
    method: 'POST',
    explicitBaseUrl,
    operatorSecret,
    errorLabel: 'run bot timer',
  });
}

export async function deleteBotTimer(id: string, explicitBaseUrl?: string, operatorSecret?: string) {
  await requestJSON(`/api/bot/timers/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    explicitBaseUrl,
    operatorSecret,
    expectJson: false,
    errorLabel: 'delete bot timer',
  });
}

export async function fetchBotCounters(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotCountersResponse>('/api/bot/counters', {
    method: 'GET',
    explicitBaseUrl,
    operatorSecret,
    errorLabel: 'bot counters',
  });
}

export async function saveBotCounter(payload: BotCounterPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotCounter>('/api/bot/counters', {
    method: 'POST',
    explicitBaseUrl,
    operatorSecret,
    body: payload,
    errorLabel: 'save bot counter',
  });
}

export async function deleteBotCounter(id: string, explicitBaseUrl?: string, operatorSecret?: string) {
  await requestJSON(`/api/bot/counters/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    explicitBaseUrl,
    operatorSecret,
    expectJson: false,
    errorLabel: 'delete bot counter',
  });
}

export async function fetchBotQuotes(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotQuotesResponse>('/api/bot/quotes', {
    method: 'GET',
    explicitBaseUrl,
    operatorSecret,
    errorLabel: 'bot quotes',
  });
}

export async function fetchBotAPIVariables(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotAPIVariablesResponse>('/api/bot/api-variables', {
    method: 'GET',
    explicitBaseUrl,
    operatorSecret,
    errorLabel: 'bot api variables',
  });
}

export async function saveBotAPIVariable(payload: BotAPIVariablePayload, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotAPIVariable>('/api/bot/api-variables', {
    method: 'POST',
    explicitBaseUrl,
    operatorSecret,
    body: payload,
    errorLabel: 'save bot api variable',
  });
}

export async function deleteBotAPIVariable(id: string, explicitBaseUrl?: string, operatorSecret?: string) {
  await requestJSON(`/api/bot/api-variables/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    explicitBaseUrl,
    operatorSecret,
    expectJson: false,
    errorLabel: 'delete bot api variable',
  });
}

export async function saveBotQuote(payload: BotQuotePayload, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotQuote>('/api/bot/quotes', {
    method: 'POST',
    explicitBaseUrl,
    operatorSecret,
    body: payload,
    errorLabel: 'save bot quote',
  });
}

export async function deleteBotQuote(id: string, explicitBaseUrl?: string, operatorSecret?: string) {
  await requestJSON(`/api/bot/quotes/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    explicitBaseUrl,
    operatorSecret,
    expectJson: false,
    errorLabel: 'delete bot quote',
  });
}

export async function fetchBotModerationRules(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotModerationRulesResponse>('/api/bot/moderation/rules', {
    method: 'GET',
    explicitBaseUrl,
    operatorSecret,
    errorLabel: 'bot moderation rules',
  });
}

export async function saveBotModerationRule(payload: BotModerationRulePayload, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotModerationRule>('/api/bot/moderation/rules', {
    method: 'POST',
    explicitBaseUrl,
    operatorSecret,
    body: payload,
    errorLabel: 'save bot moderation rule',
  });
}

export async function deleteBotModerationRule(id: string, explicitBaseUrl?: string, operatorSecret?: string) {
  await requestJSON(`/api/bot/moderation/rules/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    explicitBaseUrl,
    operatorSecret,
    expectJson: false,
    errorLabel: 'bot moderation rule delete',
  });
}

export async function dryRunBotModeration(payload: { message: string; username: string; role?: string }, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotModerationDryRunResponse>('/api/bot/moderation/dry-run', {
    method: 'POST',
    explicitBaseUrl,
    operatorSecret,
    body: payload,
    errorLabel: 'dry run bot moderation',
  });
}

export async function executeBotModerationAction(payload: BotModerationActionPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotModerationActionResponse>('/api/bot/moderation/action', {
    method: 'POST',
    explicitBaseUrl,
    operatorSecret,
    body: payload,
    errorLabel: 'execute bot moderation action',
  });
}

export async function dryRunBotNuke(payload: BotNukeDryRunPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotNukeDryRunResponse>('/api/bot/moderation/nuke/dry-run', {
    method: 'POST',
    explicitBaseUrl,
    operatorSecret,
    body: payload,
    errorLabel: 'dry run bot nuke',
  });
}

export async function executeBotNuke(payload: BotNukeDryRunPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotNukeExecuteResponse>('/api/bot/moderation/nuke/execute', {
    method: 'POST',
    explicitBaseUrl,
    operatorSecret,
    body: payload,
    errorLabel: 'execute bot nuke',
  });
}

export async function rollbackBotNukeRadiation(ruleId: string, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<BotNukeRollbackResponse>('/api/bot/moderation/nuke/radiation/rollback', {
    method: 'POST',
    explicitBaseUrl,
    operatorSecret,
    body: { ruleId, confirm: true },
    errorLabel: 'rollback bot nuke radiation',
  });
}

export async function fetchTwitchLoginURL(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<{ authorizeUrl: string }>('/api/auth/twitch/login-url', { method: 'POST', explicitBaseUrl, operatorSecret, body: {}, errorLabel: '/api/auth/twitch/login-url' });
}

export async function fetchTwitchInteractions(explicitBaseUrl?: string, operatorSecret?: string): Promise<TwitchInteractionsResponse>;
export async function fetchTwitchInteractions(options?: FetchTwitchInteractionsOptions): Promise<TwitchInteractionsResponse>;
export async function fetchTwitchInteractions(
  explicitBaseUrlOrOptions?: string | FetchTwitchInteractionsOptions,
  operatorSecret?: string,
) {
  const explicitBaseUrl = typeof explicitBaseUrlOrOptions === 'string' ? explicitBaseUrlOrOptions : explicitBaseUrlOrOptions?.baseUrl;
  const secret = operatorSecret ?? (typeof explicitBaseUrlOrOptions === 'string' ? undefined : explicitBaseUrlOrOptions?.operatorSecret);
  return requestJSON<TwitchInteractionsResponse>('/api/twitch/interactions', {
    method: 'GET',
    explicitBaseUrl,
    operatorSecret: secret,
    errorLabel: 'Twitch interactions',
  });
}

export async function fetchTwitchRewards(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<TwitchRewardsResponse>('/api/twitch/rewards', {
    method: 'GET',
    explicitBaseUrl,
    operatorSecret,
    errorLabel: 'Twitch rewards',
  });
}

export async function createTwitchReward(payload: TwitchRewardCreatePayload, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<TwitchRewardMutationResponse>('/api/twitch/rewards', {
    method: 'POST',
    explicitBaseUrl,
    operatorSecret,
    body: payload,
    errorLabel: 'create Twitch reward',
  });
}

export async function deleteTwitchReward(rewardId: string, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<TwitchRewardMutationResponse>(`/api/twitch/rewards/${encodeURIComponent(rewardId)}`, {
    method: 'DELETE',
    explicitBaseUrl,
    operatorSecret,
    body: { confirm: true },
    includeDetail: true,
    errorLabel: 'Twitch reward delete',
  });
}

export async function updateTwitchReward(
  rewardId: string,
  payload: TwitchRewardUpdatePayload,
  explicitBaseUrl?: string,
  operatorSecret?: string,
) {
  return requestJSON<TwitchRewardMutationResponse>(`/api/twitch/rewards/${encodeURIComponent(rewardId)}`, {
    method: 'PATCH',
    explicitBaseUrl,
    operatorSecret,
    body: payload,
    errorLabel: 'update Twitch reward',
  });
}

export async function fetchTwitchRewardRedemptions(rewardId: string, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<TwitchRedemptionsResponse>(`/api/twitch/rewards/${encodeURIComponent(rewardId)}/redemptions?status=UNFULFILLED`, {
    method: 'GET',
    explicitBaseUrl,
    operatorSecret,
    errorLabel: 'Twitch redemptions',
  });
}

export async function updateTwitchRedemptionStatus(rewardId: string, redemptionId: string, status: 'FULFILLED' | 'CANCELED', explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<{ status: string; result: string }>(`/api/twitch/rewards/${encodeURIComponent(rewardId)}/redemptions/${encodeURIComponent(redemptionId)}`, {
    method: 'POST',
    explicitBaseUrl,
    operatorSecret,
    body: { status },
    errorLabel: 'update Twitch redemption status',
  });
}

export async function fetchOBSStatus(explicitBaseUrl?: string) {
  return requestJSON<OBSStatus>('/api/obs/status', { method: 'GET', explicitBaseUrl, errorLabel: 'OBS status' });
}

export async function fetchReadiness(explicitBaseUrl?: string) {
  return requestJSON<ReadinessReport>('/readyz', { method: 'GET', explicitBaseUrl, errorLabel: 'Readiness' });
}

export async function fetchBuildInfo(explicitBaseUrl?: string) {
  return requestJSON<BuildInfo>('/version', { method: 'GET', explicitBaseUrl, errorLabel: 'Build info' });
}

export async function fetchEventHistory(limit = 24, explicitBaseUrl?: string, operatorSecret?: string) {
  const boundedLimit = Math.max(1, Math.min(limit, 250));
  return requestJSON<EventHistoryResponse>(`/api/events/history?limit=${boundedLimit}`, { method: 'GET', explicitBaseUrl, operatorSecret, errorLabel: 'Event history' });
}

export async function fetchStreamSession(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<StreamSessionResponse>('/api/stream/session', { method: 'GET', explicitBaseUrl, operatorSecret, errorLabel: 'Stream session status' });
}

export async function fetchStreamSessionReport(sessionId: string, limit = 100, explicitBaseUrl?: string, operatorSecret?: string) {
  const boundedLimit = Math.max(1, Math.min(limit, 250));
  return requestJSON<StreamSessionReport>(`/api/stream/session/${encodeURIComponent(sessionId)}?limit=${boundedLimit}`, { method: 'GET', explicitBaseUrl, operatorSecret, errorLabel: 'Stream session report' });
}

export async function startStreamSession(payload: { title: string; kind: 'rehearsal' | 'live'; notes?: string }, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<StreamSession>('/api/stream/session/start', { method: 'POST', explicitBaseUrl, operatorSecret, body: payload, errorLabel: '/api/stream/session/start' });
}

export async function endStreamSession(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<StreamSession | StreamSessionResponse>('/api/stream/session/end', { method: 'POST', explicitBaseUrl, operatorSecret, body: {}, errorLabel: '/api/stream/session/end' });
}

export async function fetchDashboardConfig(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<DashboardConfigResponse>('/api/dashboard/config', { method: 'GET', explicitBaseUrl, operatorSecret, errorLabel: 'Dashboard config fetch' });
}

export async function saveSharedDashboardConfig(config: DashboardConfig, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<DashboardConfigResponse>('/api/dashboard/config', { method: 'PUT', explicitBaseUrl, operatorSecret, body: { config }, errorLabel: '/api/dashboard/config' });
}

export async function fetchOBSScenes(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<OBSSceneList>('/api/obs/scenes', { method: 'GET', explicitBaseUrl, operatorSecret, errorLabel: 'OBS scenes' });
}

export async function fetchOBSInputs(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<OBSInput[]>('/api/obs/inputs', { method: 'GET', explicitBaseUrl, operatorSecret, errorLabel: 'OBS inputs' });
}

export async function fetchOBSSceneItems(sceneName: string, explicitBaseUrl?: string, operatorSecret?: string) {
  const trimmedSceneName = sceneName.trim();
  return requestJSON<OBSSceneItem[]>(`/api/obs/scene-items?sceneName=${encodeURIComponent(trimmedSceneName)}`, { method: 'GET', explicitBaseUrl, operatorSecret, errorLabel: 'OBS scene items' });
}

export async function setOBSScene(sceneName: string, explicitBaseUrl?: string, operatorSecret?: string) {
  await requestJSON('/api/obs/scene', { method: 'POST', explicitBaseUrl, operatorSecret, body: { sceneName }, errorLabel: '/api/obs/scene', expectJson: false });
}

export async function setOBSInputMute(inputName: string, muted: boolean, explicitBaseUrl?: string, operatorSecret?: string) {
  await requestJSON('/api/obs/input/mute', { method: 'POST', explicitBaseUrl, operatorSecret, body: { inputName, muted }, errorLabel: '/api/obs/input/mute', expectJson: false });
}

export async function setOBSSceneItemEnabled(sceneName: string, sceneItemId: number, enabled: boolean, explicitBaseUrl?: string, operatorSecret?: string) {
  await requestJSON('/api/obs/scene-item/enabled', { method: 'POST', explicitBaseUrl, operatorSecret, body: { sceneName, sceneItemId, enabled }, errorLabel: '/api/obs/scene-item/enabled', expectJson: false });
}

export async function refreshOBSSource(inputName: string, explicitBaseUrl?: string, operatorSecret?: string) {
  await requestJSON('/api/obs/source/refresh', { method: 'POST', explicitBaseUrl, operatorSecret, body: { inputName }, errorLabel: '/api/obs/source/refresh', expectJson: false });
}

export async function sendTestAlert(payload: AlertPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  await requestJSON('/api/events/test-alert', { method: 'POST', explicitBaseUrl, operatorSecret, body: payload, errorLabel: '/api/events/test-alert', expectJson: false });
}

export async function clearOverlays(explicitBaseUrl?: string, operatorSecret?: string) {
  await requestJSON('/api/events/clear', { method: 'POST', explicitBaseUrl, operatorSecret, errorLabel: 'Clear overlays', expectJson: false });
}

export async function setBRBStatus(payload: BRBPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  await requestJSON('/api/events/brb', { method: 'POST', explicitBaseUrl, operatorSecret, body: payload, errorLabel: '/api/events/brb', expectJson: false });
}

export async function saveTwitchInteractions(payload: TwitchInteractionRules, explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<TwitchInteractionsResponse>('/api/twitch/interactions', {
    method: 'PUT',
    explicitBaseUrl,
    operatorSecret,
    body: { rules: payload },
    errorLabel: 'save Twitch interactions',
  });
}

export async function sendWebhook(source: string, payload: AlertPayload, explicitBaseUrl?: string, webhookSecret?: string) {
  await requestJSON(`/api/integrations/webhook/${encodeURIComponent(source)}`, { method: 'POST', explicitBaseUrl, webhookSecret, body: payload, errorLabel: `/api/integrations/webhook/${encodeURIComponent(source)}`, expectJson: false });
}

export async function fetchOverlayToken(explicitBaseUrl?: string, operatorSecret?: string) {
  return requestJSON<{ key: string }>('/api/dashboard/overlay-token', { method: 'GET', explicitBaseUrl, operatorSecret, errorLabel: 'Overlay token' });
}
