import { getOverlayApiBaseUrl } from './eventStream';

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
};

export type AlertPayload = {
  title: string;
  message: string;
  severity: 'info' | 'success' | 'warning' | 'error';
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

export type OBSScene = {
  sceneIndex: number;
  sceneName: string;
  sceneUuid?: string;
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

export async function fetchOBSStatus(explicitBaseUrl?: string) {
  const response = await fetch(apiUrl('/api/obs/status', explicitBaseUrl), { headers: { Accept: 'application/json' } });
  if (!response.ok) {
    throw new Error(`OBS status failed: ${response.status}`);
  }
  return (await response.json()) as OBSStatus;
}

export async function fetchOBSScenes(explicitBaseUrl?: string) {
  const response = await fetch(apiUrl('/api/obs/scenes', explicitBaseUrl), { headers: { Accept: 'application/json' } });
  if (!response.ok) {
    throw new Error(`OBS scenes failed: ${response.status}`);
  }
  return (await response.json()) as OBSSceneList;
}

export async function setOBSScene(sceneName: string, explicitBaseUrl?: string) {
  await postJSON('/api/obs/scene', { sceneName }, explicitBaseUrl);
}

export async function setOBSInputMute(inputName: string, muted: boolean, explicitBaseUrl?: string) {
  await postJSON('/api/obs/input/mute', { inputName, muted }, explicitBaseUrl);
}

export async function setOBSSceneItemEnabled(sceneName: string, sceneItemId: number, enabled: boolean, explicitBaseUrl?: string) {
  await postJSON('/api/obs/scene-item/enabled', { sceneName, sceneItemId, enabled }, explicitBaseUrl);
}

export async function sendTestAlert(payload: AlertPayload, explicitBaseUrl?: string) {
  await postJSON('/api/events/test-alert', payload, explicitBaseUrl);
}

export async function clearOverlays(explicitBaseUrl?: string) {
  const response = await fetch(apiUrl('/api/events/clear', explicitBaseUrl), { method: 'POST' });
  if (!response.ok) {
    throw new Error(`Clear overlays failed: ${response.status}`);
  }
}

export async function sendWebhook(source: string, payload: AlertPayload, explicitBaseUrl?: string, webhookSecret?: string) {
  await postJSON(`/api/integrations/webhook/${encodeURIComponent(source)}`, payload, explicitBaseUrl, webhookSecret);
}

async function postJSON(path: string, payload: Record<string, unknown>, explicitBaseUrl?: string, webhookSecret?: string) {
	const headers: Record<string, string> = { 'Content-Type': 'application/json', Accept: 'application/json' };
	if (webhookSecret?.trim()) {
		headers['X-Overlay-Webhook-Secret'] = webhookSecret.trim();
	}

  const response = await fetch(apiUrl(path, explicitBaseUrl), {
    method: 'POST',
    headers,
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    throw new Error(`${path} failed: ${response.status}`);
  }
}
