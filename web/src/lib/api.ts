import { getOverlayApiBaseUrl } from './eventStream';
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

export type EventHistoryResponse = {
  events: EventHistoryItem[];
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

export async function fetchEventHistory(limit = 24, explicitBaseUrl?: string) {
  const boundedLimit = Math.max(1, Math.min(limit, 250));
  const response = await fetch(apiUrl(`/api/events/history?limit=${boundedLimit}`, explicitBaseUrl), { headers: { Accept: 'application/json' } });
  if (!response.ok) {
    throw new Error(`Event history failed: ${response.status}`);
  }
  return (await response.json()) as EventHistoryResponse;
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

export async function sendWebhook(source: string, payload: AlertPayload, explicitBaseUrl?: string, webhookSecret?: string) {
  await postJSON(`/api/integrations/webhook/${encodeURIComponent(source)}`, payload, explicitBaseUrl, { webhookSecret });
}

type PostJSONOptions = {
  operatorSecret?: string;
  webhookSecret?: string;
};

async function postJSON(path: string, payload: Record<string, unknown>, explicitBaseUrl?: string, options: PostJSONOptions = {}) {
  const headers: Record<string, string> = { 'Content-Type': 'application/json', Accept: 'application/json' };
  if (options.operatorSecret?.trim()) {
    headers['X-Overlay-Operator-Secret'] = options.operatorSecret.trim();
  }
  if (options.webhookSecret?.trim()) {
    headers['X-Overlay-Webhook-Secret'] = options.webhookSecret.trim();
  }

  const response = await fetch(apiUrl(path, explicitBaseUrl), {
    method: 'POST',
    headers,
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    throw new Error(`${path} failed: ${response.status}`);
  }
  return response;
}
