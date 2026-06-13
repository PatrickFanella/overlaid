export type OverlayEventType =
  | 'alert'
  | 'clear'
  | 'service_status'
  | 'chat_message'
  | 'custom_command'
  | 'now_playing'
  | 'brb';

export type OverlaySeverity = 'info' | 'success' | 'warning' | 'error';

export type CommandEventData = {
  command: string;
  args: string;
  user: string;
  login: string;
  isBroadcaster: boolean;
  isModerator: boolean;
};

export type OverlayEvent = {
  id: string;
  type: OverlayEventType;
  source: string;
  title: string;
  message: string;
  severity: OverlaySeverity;
  createdAt: string;
  data?: Record<string, unknown>;
};

const eventTypes = new Set<OverlayEventType>([
  'alert',
  'clear',
  'service_status',
  'chat_message',
  'custom_command',
  'now_playing',
  'brb',
]);

const severities = new Set<OverlaySeverity>(['info', 'success', 'warning', 'error']);

export function parseOverlayEvent(payload: string | Record<string, unknown>, fallbackType?: string): OverlayEvent | null {
  const raw = typeof payload === 'string' ? safeJsonParse(payload) : payload;

  if (!isRecord(raw)) {
    return null;
  }

  const type = parseEventType(raw.type, fallbackType);
  const source = asString(raw.source) || 'manual';
  const id = asString(raw.id) || `${type}-${source}-${Date.now().toString(36)}`;
  const severity = parseSeverity(raw.severity);

  return {
    id,
    type,
    source,
    title: asString(raw.title),
    message: asString(raw.message),
    severity,
    createdAt: asString(raw.createdAt) || new Date().toISOString(),
    data: isRecord(raw.data) ? raw.data : undefined,
  };
}

export function formatOverlayStreamLabel(state: 'connecting' | 'live' | 'error') {
  switch (state) {
    case 'live':
      return 'live';
    case 'error':
      return 'reconnecting';
    default:
      return 'connecting';
  }
}

function parseEventType(value: unknown, fallbackType?: string): OverlayEventType {
  if (typeof value === 'string' && eventTypes.has(value as OverlayEventType)) {
    return value as OverlayEventType;
  }

  if (typeof fallbackType === 'string' && eventTypes.has(fallbackType as OverlayEventType)) {
    return fallbackType as OverlayEventType;
  }

  return 'alert';
}

function parseSeverity(value: unknown): OverlaySeverity {
  if (typeof value === 'string' && severities.has(value as OverlaySeverity)) {
    return value as OverlaySeverity;
  }

  return 'info';
}

function safeJsonParse(value: string) {
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return null;
  }
}

function asString(value: unknown) {
  return typeof value === 'string' ? value.trim() : '';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
