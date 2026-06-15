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
  eventSub?: {
    enabled: boolean;
    connected: boolean;
    lastConnectedAt?: string;
    lastEventAt?: string;
    lastError?: string;
    subscriptionsMade: number;
    subscriptionsPlan: number;
  };
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

export async function fetchOBSScenes(explicitBaseUrl?: string) {
  const response = await fetch(apiUrl('/api/obs/scenes', explicitBaseUrl), { headers: { Accept: 'application/json' } });
  if (!response.ok) {
    throw new Error(`OBS scenes failed: ${response.status}`);
  }
  return (await response.json()) as OBSSceneList;
}

export async function fetchOBSInputs(explicitBaseUrl?: string) {
  const response = await fetch(apiUrl('/api/obs/inputs', explicitBaseUrl), { headers: { Accept: 'application/json' } });
  if (!response.ok) {
    throw new Error(`OBS inputs failed: ${response.status}`);
  }
  return (await response.json()) as OBSInput[];
}

export async function fetchOBSSceneItems(sceneName: string, explicitBaseUrl?: string) {
  const trimmedSceneName = sceneName.trim();
  const response = await fetch(apiUrl(`/api/obs/scene-items?sceneName=${encodeURIComponent(trimmedSceneName)}`, explicitBaseUrl), { headers: { Accept: 'application/json' } });
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
  method: 'POST' | 'PUT',
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
