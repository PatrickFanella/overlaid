import { parseOverlayEvent, type OverlayEvent } from './events';

export const DEFAULT_API_BASE_URL = 'http://localhost:8080';

export type OverlayStreamStatus = 'connecting' | 'live' | 'error';

export type OverlayStreamHandlers = {
  onEvent: (event: OverlayEvent) => void;
  onStatusChange?: (status: OverlayStreamStatus) => void;
};

const overlayEventTypes = ['alert', 'clear', 'service_status', 'chat_message', 'custom_command', 'now_playing', 'brb'] as const;

export function getOverlayApiBaseUrl(explicitBaseUrl?: string) {
  return normalizeBaseUrl(explicitBaseUrl || getQueryApiBaseUrl() || import.meta.env.VITE_API_BASE_URL || getDefaultApiBaseUrl());
}

export function getDefaultApiBaseUrl(origin?: string) {
  const runtimeOrigin = origin || getRuntimeOrigin();
  if (!runtimeOrigin) {
    return DEFAULT_API_BASE_URL;
  }

  const url = new URL(runtimeOrigin);
  url.port = '8080';
  return url.toString().replace(/\/+$/, '');
}

export function getOverlayStreamUrl(explicitBaseUrl?: string) {
  return new URL('/api/events/stream', getOverlayApiBaseUrl(explicitBaseUrl)).toString();
}

export function connectOverlayStream(explicitBaseUrl: string | undefined, handlers: OverlayStreamHandlers) {
  const source = new EventSource(getOverlayStreamUrl(explicitBaseUrl));

  source.addEventListener('open', () => {
    handlers.onStatusChange?.('live');
  });

  source.addEventListener('error', () => {
    handlers.onStatusChange?.('error');
  });

  for (const type of overlayEventTypes) {
    source.addEventListener(type, (event) => {
      const parsed = parseOverlayEvent((event as MessageEvent<string>).data, type);
      if (parsed) {
        handlers.onEvent(parsed);
      }
    });
  }

  source.addEventListener('message', (event) => {
    const parsed = parseOverlayEvent((event as MessageEvent<string>).data);
    if (parsed) {
      handlers.onEvent(parsed);
    }
  });

  return source;
}

function normalizeBaseUrl(baseUrl: string) {
  return baseUrl.replace(/\/+$/, '');
}

function getRuntimeOrigin() {
  if (typeof window === 'undefined') {
    return '';
  }

  return window.location.origin;
}

function getQueryApiBaseUrl() {
  if (typeof window === 'undefined') {
    return '';
  }

  return new URLSearchParams(window.location.search).get('apiBaseUrl') || '';
}
