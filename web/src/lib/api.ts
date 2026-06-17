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
  const response = await fetch(apiUrl('/api/bot/status', explicitBaseUrl), { headers: operatorHeaders(operatorSecret) });
  if (!response.ok) {
    throw new Error(`bot status failed: ${response.status}`);
  }
  return (await response.json()) as BotStatus;
}

export async function fetchBotAudit(limit = 50, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl(`/api/bot/audit?limit=${encodeURIComponent(String(limit))}`, explicitBaseUrl), { headers: operatorHeaders(operatorSecret) });
  if (!response.ok) {
    throw new Error(`bot audit failed: ${response.status}`);
  }
  return (await response.json()) as BotAuditResponse;
}

export async function fetchBotTimers(explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl('/api/bot/timers', explicitBaseUrl), { headers: operatorHeaders(operatorSecret) });
  if (!response.ok) {
    throw new Error(`bot timers failed: ${response.status}`);
  }
  return (await response.json()) as BotTimersResponse;
}

export async function fetchBotCommands(explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl('/api/bot/commands', explicitBaseUrl), { headers: operatorHeaders(operatorSecret) });
  if (!response.ok) {
    throw new Error(`bot commands failed: ${response.status}`);
  }
  return (await response.json()) as BotCommandsResponse;
}

export async function saveBotCommand(payload: BotCommandPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON('/api/bot/commands', payload, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as BotCommand;
}

export async function setBotCommandEnabled(id: string, enabled: boolean, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await sendJSON('PUT', `/api/bot/commands/${encodeURIComponent(id)}`, { enabled }, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as BotCommand;
}

export async function deleteBotCommand(id: string, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl(`/api/bot/commands/${encodeURIComponent(id)}`, explicitBaseUrl), { method: 'DELETE', headers: operatorHeaders(operatorSecret) });
  if (!response.ok) {
    const detail = await response.text().catch(() => '');
    throw new Error(`delete bot command failed: ${response.status}${detail.trim() ? ` — ${detail.trim()}` : ''}`);
  }
}

export async function saveBotTimer(payload: BotTimerPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON('/api/bot/timers', payload, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as BotTimer;
}

export async function setBotTimerEnabled(id: string, enabled: boolean, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await sendJSON('PUT', `/api/bot/timers/${encodeURIComponent(id)}`, { enabled }, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as BotTimer;
}

export async function deleteBotTimer(id: string, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl(`/api/bot/timers/${encodeURIComponent(id)}`, explicitBaseUrl), { method: 'DELETE', headers: operatorHeaders(operatorSecret) });
  if (!response.ok) {
    const detail = await response.text().catch(() => '');
    throw new Error(`delete bot timer failed: ${response.status}${detail.trim() ? ` — ${detail.trim()}` : ''}`);
  }
}

export async function fetchBotCounters(explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl('/api/bot/counters', explicitBaseUrl), { headers: operatorHeaders(operatorSecret) });
  if (!response.ok) throw new Error(`bot counters failed: ${response.status}`);
  return (await response.json()) as BotCountersResponse;
}

export async function saveBotCounter(payload: BotCounterPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON('/api/bot/counters', payload, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as BotCounter;
}

export async function deleteBotCounter(id: string, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl(`/api/bot/counters/${encodeURIComponent(id)}`, explicitBaseUrl), { method: 'DELETE', headers: operatorHeaders(operatorSecret) });
  if (!response.ok) throw new Error(`delete bot counter failed: ${response.status}`);
}

export async function fetchBotQuotes(explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl('/api/bot/quotes', explicitBaseUrl), { headers: operatorHeaders(operatorSecret) });
  if (!response.ok) throw new Error(`bot quotes failed: ${response.status}`);
  return (await response.json()) as BotQuotesResponse;
}

export async function fetchBotAPIVariables(explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl('/api/bot/api-variables', explicitBaseUrl), { headers: operatorHeaders(operatorSecret) });
  if (!response.ok) throw new Error(`bot api variables failed: ${response.status}`);
  return (await response.json()) as BotAPIVariablesResponse;
}

export async function saveBotAPIVariable(payload: BotAPIVariablePayload, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON('/api/bot/api-variables', payload, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as BotAPIVariable;
}

export async function deleteBotAPIVariable(id: string, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl(`/api/bot/api-variables/${encodeURIComponent(id)}`, explicitBaseUrl), { method: 'DELETE', headers: operatorHeaders(operatorSecret) });
  if (!response.ok) throw new Error(`delete bot api variable failed: ${response.status}`);
}

export async function saveBotQuote(payload: BotQuotePayload, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON('/api/bot/quotes', payload, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as BotQuote;
}

export async function deleteBotQuote(id: string, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl(`/api/bot/quotes/${encodeURIComponent(id)}`, explicitBaseUrl), { method: 'DELETE', headers: operatorHeaders(operatorSecret) });
  if (!response.ok) throw new Error(`delete bot quote failed: ${response.status}`);
}

export async function fetchBotModerationRules(explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl('/api/bot/moderation/rules', explicitBaseUrl), { headers: operatorHeaders(operatorSecret) });
  if (!response.ok) throw new Error(`bot moderation rules failed: ${response.status}`);
  return (await response.json()) as BotModerationRulesResponse;
}

export async function saveBotModerationRule(payload: BotModerationRulePayload, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON('/api/bot/moderation/rules', payload, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as BotModerationRule;
}

export async function deleteBotModerationRule(id: string, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl(`/api/bot/moderation/rules/${encodeURIComponent(id)}`, explicitBaseUrl), { method: 'DELETE', headers: operatorHeaders(operatorSecret) });
  if (!response.ok) throw new Error(`bot moderation rule delete failed: ${response.status}`);
}

export async function dryRunBotModeration(payload: { message: string; username: string }, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON('/api/bot/moderation/dry-run', payload, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as BotModerationDryRunResponse;
}

export async function executeBotModerationAction(payload: BotModerationActionPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON('/api/bot/moderation/action', payload, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as BotModerationActionResponse;
}

export async function dryRunBotNuke(payload: BotNukeDryRunPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON('/api/bot/moderation/nuke/dry-run', payload, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as BotNukeDryRunResponse;
}

export async function executeBotNuke(payload: BotNukeDryRunPayload, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON('/api/bot/moderation/nuke/execute', payload, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as BotNukeExecuteResponse;
}

export async function rollbackBotNukeRadiation(ruleId: string, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON('/api/bot/moderation/nuke/radiation/rollback', { ruleId, confirm: true }, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as BotNukeRollbackResponse;
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
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (secret?.trim()) {
    headers['X-Overlay-Operator-Secret'] = secret.trim();
  }

  const response = await fetch(apiUrl('/api/twitch/interactions', explicitBaseUrl), { headers });
  if (!response.ok) {
    throw new Error(`Twitch interactions failed: ${response.status}`);
  }

  return (await response.json()) as TwitchInteractionsResponse;
}

export async function fetchTwitchRewards(explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl('/api/twitch/rewards', explicitBaseUrl), { headers: operatorHeaders(operatorSecret) });
  if (!response.ok) throw new Error(`Twitch rewards failed: ${response.status}`);
  return (await response.json()) as TwitchRewardsResponse;
}

export async function createTwitchReward(payload: TwitchRewardCreatePayload, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON('/api/twitch/rewards', payload, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as TwitchRewardMutationResponse;
}

export async function deleteTwitchReward(rewardId: string, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl(`/api/twitch/rewards/${encodeURIComponent(rewardId)}`, explicitBaseUrl), {
    method: 'DELETE',
    headers: { ...operatorHeaders(operatorSecret), 'Content-Type': 'application/json' },
    body: JSON.stringify({ confirm: true }),
  });
  if (!response.ok) throw new Error(`Twitch reward delete failed: ${response.status}`);
  return (await response.json()) as TwitchRewardMutationResponse;
}

export async function updateTwitchReward(
  rewardId: string,
  payload: TwitchRewardUpdatePayload,
  explicitBaseUrl?: string,
  operatorSecret?: string,
) {
  const response = await sendJSON('PATCH', `/api/twitch/rewards/${encodeURIComponent(rewardId)}`, payload, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as TwitchRewardMutationResponse;
}

export async function fetchTwitchRewardRedemptions(rewardId: string, explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await fetch(apiUrl(`/api/twitch/rewards/${encodeURIComponent(rewardId)}/redemptions?status=UNFULFILLED`, explicitBaseUrl), { headers: operatorHeaders(operatorSecret) });
  if (!response.ok) throw new Error(`Twitch redemptions failed: ${response.status}`);
  return (await response.json()) as TwitchRedemptionsResponse;
}

export async function updateTwitchRedemptionStatus(rewardId: string, redemptionId: string, status: 'FULFILLED' | 'CANCELED', explicitBaseUrl?: string, operatorSecret?: string) {
  const response = await postJSON(`/api/twitch/rewards/${encodeURIComponent(rewardId)}/redemptions/${encodeURIComponent(redemptionId)}`, { status }, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as { status: string; result: string };
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
  const response = await sendJSON('PUT', '/api/twitch/interactions', { rules: payload }, explicitBaseUrl, { operatorSecret });
  return (await response.json()) as TwitchInteractionsResponse;
}

export async function sendWebhook(source: string, payload: AlertPayload, explicitBaseUrl?: string, webhookSecret?: string) {
  await postJSON(`/api/integrations/webhook/${encodeURIComponent(source)}`, payload, explicitBaseUrl, { webhookSecret });
}

type PostJSONOptions = {
  operatorSecret?: string;
  webhookSecret?: string;
};

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
