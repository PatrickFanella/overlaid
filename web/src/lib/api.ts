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
export type BotModerationRule = { id: string; name: string; enabled: boolean; enforce?: boolean; pattern: string; matchTarget: 'message' | 'username' | 'both' | string; caseSensitive: boolean; regex: boolean; action: 'flag' | 'delete' | 'timeout' | 'ban' | string; reason?: string; source?: string; expiresAt?: string; createdAt: string; updatedAt: string };
export type BotModerationRulesResponse = { rules: BotModerationRule[]; loadError?: string };
export type BotModerationRulePayload = { id?: string; name: string; enabled: boolean; enforce?: boolean; pattern: string; matchTarget: 'message' | 'username' | 'both'; caseSensitive: boolean; regex: boolean; action: 'flag' | 'delete' | 'timeout' | 'ban'; reason?: string };
export type BotModerationMatch = { ruleId: string; ruleName: string; pattern: string; matchTarget: string; action: string; reason?: string };
export type BotModerationDryRunResponse = { matches: BotModerationMatch[]; loadError?: string };
export type BotModerationActionPayload = { action: 'delete' | 'timeout' | 'ban' | 'unban'; targetUserId?: string; targetUserLogin?: string; messageId?: string; durationSeconds?: number; reason?: string };
export type BotModerationActionResponse = { action: string; result: string; reason?: string };
export type BotNukeDryRunPayload = { pattern: string; regex: boolean; caseSensitive: boolean; lookbackMinutes: number; limit: number; action?: 'delete' | 'timeout'; durationSeconds?: number; confirm?: boolean; radiationMinutes?: number };
export type BotNukeDryRunMatch = { eventId: string; messageId?: string; userId?: string; user: string; message: string; createdAt: string };
export type BotNukeDryRunResponse = { matches: BotNukeDryRunMatch[]; pattern: string; action: string };
export type BotNukeExecuteResponse = { matches: BotNukeDryRunMatch[]; attempted: number; succeeded: number; failed: number; action: string; radiationRule?: BotModerationRule };
export type BotNukeRollbackResponse = { rule: BotModerationRule };

export type BotCommandPayload = {
  id?: string;
  name: string;
  enabled: boolean;
  response: string;
  action: 'ticker' | 'alert';
  responseMode: 'overlay' | 'chat' | 'both';
  roleRequired: 'viewer' | 'moderator' | 'broadcaster';
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

export type TwitchReward = { id: string; title: string; prompt?: string; cost: number; is_enabled?: boolean; is_paused?: boolean; is_in_stock?: boolean; manageable?: boolean };
export type TwitchRewardsResponse = { rewards: TwitchReward[] };
export type TwitchRewardCreatePayload = { title: string; prompt?: string; cost: number; enabled: boolean; userInputRequired: boolean };
export type TwitchRewardUpdatePayload = { enabled?: boolean; paused?: boolean };
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

export type StreamSessionReport = {
  session: StreamSession;
  events: EventHistoryItem[];
  snapshots: StreamSessionReadinessSnapshot[];
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
  const response = await fetch(apiUrl('/api/auth/twitch/status', explicitBaseUrl), { headers: { Accept: 'application/json' } });
  if (!response.ok) {
    throw new Error(`Twitch status failed: ${response.status}`);
  }
  return (await response.json()) as TwitchStatus;
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

export async function dryRunBotModeration(payload: { message: string; username: string }, explicitBaseUrl?: string, operatorSecret?: string) {
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
  const response = await postJSON('/api/auth/twitch/login-url', {}, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as { authorizeUrl: string };
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
  const response = await fetch(apiUrl('/api/obs/status', explicitBaseUrl), { headers: { Accept: 'application/json' } });
  if (!response.ok) {
    throw new Error(`OBS status failed: ${response.status}`);
  }
  return (await response.json()) as OBSStatus;
}

export async function fetchReadiness(explicitBaseUrl?: string) {
  const response = await fetch(apiUrl('/readyz', explicitBaseUrl), { headers: { Accept: 'application/json' } });
  if (!response.ok) {
    throw new Error(`Readiness failed: ${response.status}`);
  }
  return (await response.json()) as ReadinessReport;
}

export async function fetchBuildInfo(explicitBaseUrl?: string) {
  const response = await fetch(apiUrl('/version', explicitBaseUrl), { headers: { Accept: 'application/json' } });
  if (!response.ok) {
    throw new Error(`Build info failed: ${response.status}`);
  }
  return (await response.json()) as BuildInfo;
}

export async function fetchEventHistory(limit = 24, explicitBaseUrl?: string) {
  const boundedLimit = Math.max(1, Math.min(limit, 250));
  const response = await fetch(apiUrl(`/api/events/history?limit=${boundedLimit}`, explicitBaseUrl), { headers: { Accept: 'application/json' } });
  if (!response.ok) {
    throw new Error(`Event history failed: ${response.status}`);
  }
  return (await response.json()) as EventHistoryResponse;
}

export async function fetchStreamSession(explicitBaseUrl?: string, operatorSecret?: string) {
  const headers = operatorHeaders(operatorSecret);
  const response = await fetch(apiUrl('/api/stream/session', explicitBaseUrl), { headers });
  if (!response.ok) {
    throw new Error(`Stream session status failed: ${response.status}`);
  }
  return (await response.json()) as StreamSessionResponse;
}

export async function fetchStreamSessionReport(sessionId: string, limit = 100, explicitBaseUrl?: string, operatorSecret?: string) {
  const boundedLimit = Math.max(1, Math.min(limit, 250));
  const headers = operatorHeaders(operatorSecret);
  const response = await fetch(apiUrl(`/api/stream/session/${encodeURIComponent(sessionId)}?limit=${boundedLimit}`, explicitBaseUrl), { headers });
  if (!response.ok) {
    throw new Error(`Stream session report failed: ${response.status}`);
  }
  return (await response.json()) as StreamSessionReport;
}

export async function startStreamSession(payload: { title: string; kind: 'rehearsal' | 'live'; notes?: string }, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON('/api/stream/session/start', payload, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as StreamSession;
}

export async function endStreamSession(explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON('/api/stream/session/end', {}, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as StreamSession | StreamSessionResponse;
}

export async function fetchDashboardConfig(explicitBaseUrl?: string, operatorSecret?: string) {
  const headers = operatorHeaders(operatorSecret);
  const response = await fetch(apiUrl('/api/dashboard/config', explicitBaseUrl), { headers });
  if (!response.ok) {
    throw new Error(`Dashboard config fetch failed: ${response.status}`);
  }
  return (await response.json()) as DashboardConfigResponse;
}

export async function saveSharedDashboardConfig(config: DashboardConfig, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await sendJSON('PUT', '/api/dashboard/config', { config }, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as DashboardConfigResponse;
}

export async function fetchOBSScenes(explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl('/api/obs/scenes', explicitBaseUrl), { headers: operatorHeaders(operatorSecret) });
  if (!response.ok) {
    throw new Error(`OBS scenes failed: ${response.status}`);
  }
  return (await response.json()) as OBSSceneList;
}

export async function fetchOBSInputs(explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl('/api/obs/inputs', explicitBaseUrl), { headers: operatorHeaders(operatorSecret) });
  if (!response.ok) {
    throw new Error(`OBS inputs failed: ${response.status}`);
  }
  return (await response.json()) as OBSInput[];
}

export async function fetchOBSSceneItems(sceneName: string, explicitBaseUrl?: string, operatorSecret?: string) {
  const trimmedSceneName = sceneName.trim();
  const response = await fetch(apiUrl(`/api/obs/scene-items?sceneName=${encodeURIComponent(trimmedSceneName)}`, explicitBaseUrl), { headers: operatorHeaders(operatorSecret) });
  if (!response.ok) {
    throw new Error(`OBS scene items failed: ${response.status}`);
  }
  return (await response.json()) as OBSSceneItem[];
}

export async function setOBSScene(sceneName: string, explicitBaseUrl?: string, operatorSecret?: string) {
  await postJSON('/api/obs/scene', { sceneName }, explicitBaseUrl, { operatorSecret });
}

export async function setOBSInputMute(inputName: string, muted: boolean, explicitBaseUrl?: string, operatorSecret?: string) {
  await postJSON('/api/obs/input/mute', { inputName, muted }, explicitBaseUrl, { operatorSecret });
}

export async function setOBSSceneItemEnabled(sceneName: string, sceneItemId: number, enabled: boolean, explicitBaseUrl?: string, operatorSecret?: string) {
  await postJSON('/api/obs/scene-item/enabled', { sceneName, sceneItemId, enabled }, explicitBaseUrl, { operatorSecret });
}

export async function refreshOBSSource(inputName: string, explicitBaseUrl?: string, operatorSecret?: string) {
  await postJSON('/api/obs/source/refresh', { inputName }, explicitBaseUrl, { operatorSecret });
}

export async function sendTestAlert(payload: AlertPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  await postJSON('/api/events/test-alert', payload, explicitBaseUrl, { operatorSecret });
}

export async function clearOverlays(explicitBaseUrl?: string, operatorSecret?: string) {
  const headers: Record<string, string> = {};
  if (operatorSecret?.trim()) {
    headers['X-Overlay-Operator-Secret'] = operatorSecret.trim();
  }
  const response = await fetch(apiUrl('/api/events/clear', explicitBaseUrl), { method: 'POST', headers });
  if (!response.ok) {
    throw new Error(`Clear overlays failed: ${response.status}`);
  }
}

export async function setBRBStatus(payload: BRBPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  await postJSON('/api/events/brb', payload, explicitBaseUrl, { operatorSecret });
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
  await postJSON(`/api/integrations/webhook/${encodeURIComponent(source)}`, payload, explicitBaseUrl, { webhookSecret });
}

type PostJSONOptions = {
  operatorSecret?: string;
  webhookSecret?: string;
};

type RequestJSONOptions = PostJSONOptions & {
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  body?: Record<string, unknown>;
  errorLabel: string;
  includeDetail?: boolean;
  expectJson?: boolean;
};

async function requestJSON(path: string, options: RequestJSONOptions & { explicitBaseUrl?: string; expectJson: false }): Promise<void>;
async function requestJSON<T>(path: string, options: RequestJSONOptions & { explicitBaseUrl?: string; expectJson?: true }): Promise<T>;
async function requestJSON<T>(path: string, options: RequestJSONOptions & { explicitBaseUrl?: string }) {
  const headers = operatorHeaders(options.operatorSecret);
  if (options.webhookSecret?.trim()) {
    headers['X-Overlay-Webhook-Secret'] = options.webhookSecret.trim();
  }
  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }

  const response = await fetch(apiUrl(path, options.explicitBaseUrl), {
    method: options.method,
    headers,
    ...(options.body !== undefined ? { body: JSON.stringify(options.body) } : {}),
  });

  if (!response.ok) {
    const detail = options.includeDetail === false ? '' : await response.text().catch(() => '');
    throw new Error(`${options.errorLabel} failed: ${response.status}${detail.trim() ? ` — ${detail.trim()}` : ''}`);
  }

  if (options.expectJson === false) {
    return;
  }

  return (await response.json()) as T;
}

function operatorHeaders(operatorSecret?: string) {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (operatorSecret?.trim()) {
    headers['X-Overlay-Operator-Secret'] = operatorSecret.trim();
  }
  return headers;
}

async function postJSON(path: string, payload: Record<string, unknown>, explicitBaseUrl?: string, options: PostJSONOptions = {}) {
  return sendJSON('POST', path, payload, explicitBaseUrl, options);
}

async function sendJSON(
  method: 'POST' | 'PUT' | 'PATCH',
  path: string,
  payload: Record<string, unknown>,
  explicitBaseUrl?: string,
  options: PostJSONOptions = {},
) {
  const headers: Record<string, string> = { 'Content-Type': 'application/json', Accept: 'application/json' };
  if (options.operatorSecret?.trim()) {
    headers['X-Overlay-Operator-Secret'] = options.operatorSecret.trim();
  }
  if (options.webhookSecret?.trim()) {
    headers['X-Overlay-Webhook-Secret'] = options.webhookSecret.trim();
  }

  const response = await fetch(apiUrl(path, explicitBaseUrl), {
    method,
    headers,
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    const detail = await response.text().catch(() => '');
    throw new Error(`${path} failed: ${response.status}${detail.trim() ? ` — ${detail.trim()}` : ''}`);
  }
  return response;
}
