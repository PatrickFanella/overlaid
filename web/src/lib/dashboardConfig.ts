export type SetupChecklistItem = {
  id: string;
  title: string;
  description: string;
  completed: boolean;
};

export type StreamPreset = {
  id: string;
  title: string;
  sources: string[];
};

export type ControlWidget = {
  id: string;
  title: string;
  enabled: boolean;
  order: number;
  compact: boolean;
};

export type QuickAction =
  | { id: string; label: string; kind: 'setScene'; sceneName: string }
  | { id: string; label: string; kind: 'sendAlert'; title: string; message: string; severity: 'info' | 'success' | 'warning' | 'error' }
  | { id: string; label: string; kind: 'showBRB'; title: string; message: string; eta: string; items: string[] }
  | { id: string; label: string; kind: 'hideBRB' }
  | { id: string; label: string; kind: 'clearOverlays' }
  | { id: string; label: string; kind: 'muteInput'; inputName: string; muted: boolean };

export const quickActionKinds = ['setScene', 'sendAlert', 'showBRB', 'hideBRB', 'clearOverlays', 'muteInput'] as const;

export type DashboardConfig = {
  setupChecklist: SetupChecklistItem[];
  streamPresets: StreamPreset[];
  quickActions: QuickAction[];
  controlWidgets: ControlWidget[];
  customCSS: string;
  mprisDecision: 'enabled' | 'disabled' | 'undecided';
};

const CONFIG_KEY = 'overlaid.dashboard.config.v1';
const OPERATOR_SECRET_KEY = 'overlaid.operatorSecret.session.v1';

export const defaultDashboardConfig: DashboardConfig = {
  setupChecklist: [
    { id: 'browser-sources', title: 'Browser sources added', description: 'Add every route once, then verify each source loads cleanly in OBS.', completed: false },
    { id: 'live-smoke', title: 'Live event smoke', description: 'Fire sample buttons and confirm alerts, ticker, goals, and BRB states appear.', completed: false },
    { id: 'mpris-decision', title: 'MPRIS decision', description: 'Decide whether now-playing should read from local MPRIS or stay disabled.', completed: false },
    { id: 'stream-presets', title: 'Stream presets', description: 'Pick scene-specific source sets so switching scenes is a single click.', completed: false },
    { id: 'controls-checked', title: 'Dashboard controls checked', description: 'Verify operator secret, OBS controls, and clear/reset behavior before going live.', completed: false },
  ],
  streamPresets: [
    { id: 'main', title: 'Main scene', sources: ['alerts', 'chat', 'goals', 'now-playing', 'ticker'] },
    { id: 'brb', title: 'BRB scene', sources: ['brb', 'ticker', 'now-playing'] },
    { id: 'debug', title: 'Debug scene', sources: ['debug', 'alerts', 'ticker'] },
  ],
  quickActions: [
    { id: 'main-scene', label: 'Go Main', kind: 'setScene', sceneName: 'Main' },
    { id: 'brb-scene', label: 'Go BRB', kind: 'setScene', sceneName: 'BRB' },
    { id: 'sample-alert', label: 'Sample Alert', kind: 'sendAlert', title: 'Sample alert', message: 'Quick action fired', severity: 'success' },
    { id: 'show-brb', label: 'Show BRB', kind: 'showBRB', title: 'Be right back', message: 'Short intermission. Stream resumes soon.', eta: '5 min', items: ['Hydrate', 'Stretch', 'Chat stays live'] },
    { id: 'hide-brb', label: 'Hide BRB', kind: 'hideBRB' },
  ],
  controlWidgets: [
    { id: 'quick-actions', title: 'Quick actions', enabled: true, order: 1, compact: true },
    { id: 'sample-events', title: 'Sample events', enabled: true, order: 2, compact: true },
    { id: 'brb', title: 'BRB controls', enabled: true, order: 3, compact: false },
    { id: 'obs', title: 'OBS controls', enabled: true, order: 4, compact: false },
    { id: 'editor', title: 'Quick action editor', enabled: true, order: 5, compact: false },
  ],
  customCSS: `body { background: transparent !important; }\n\n/* Compact OBS crop helper */\n#root { overflow: hidden; }`,
  mprisDecision: 'undecided',
};

export function loadDashboardConfig(): DashboardConfig {
  if (typeof window === 'undefined') return defaultDashboardConfig;
  const raw = window.localStorage.getItem(CONFIG_KEY);
  if (!raw) return defaultDashboardConfig;

  try {
    return { ...defaultDashboardConfig, ...JSON.parse(raw) } as DashboardConfig;
  } catch {
    return defaultDashboardConfig;
  }
}

export function saveDashboardConfig(config: DashboardConfig) {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(CONFIG_KEY, JSON.stringify(config));
}

export function loadOperatorSecret() {
  if (typeof window === 'undefined') return '';
  return window.sessionStorage.getItem(OPERATOR_SECRET_KEY) || '';
}

export function saveOperatorSecret(secret: string) {
  if (typeof window === 'undefined') return;
  if (secret.trim()) window.sessionStorage.setItem(OPERATOR_SECRET_KEY, secret);
  else window.sessionStorage.removeItem(OPERATOR_SECRET_KEY);
}

export function createQuickAction(kind: QuickAction['kind'], label = 'New action'): QuickAction {
  const id = createQuickActionId(kind);

  switch (kind) {
    case 'setScene':
      return { id, label, kind, sceneName: 'Main' };
    case 'sendAlert':
      return { id, label, kind, title: 'Sample alert', message: 'Quick action fired', severity: 'success' };
    case 'showBRB':
      return {
        id,
        label,
        kind,
        title: 'Be right back',
        message: 'Short intermission. Stream resumes soon.',
        eta: '5 min',
        items: ['Hydrate', 'Stretch', 'Chat stays live'],
      };
    case 'hideBRB':
      return { id, label, kind };
    case 'clearOverlays':
      return { id, label, kind };
    case 'muteInput':
      return { id, label, kind, inputName: 'Microphone', muted: true };
  }
}

function createQuickActionId(kind: QuickAction['kind']) {
  const suffix =
    typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
      ? crypto.randomUUID().slice(0, 8)
      : Math.random().toString(36).slice(2, 10);

  return `quick-${kind}-${Date.now().toString(36)}-${suffix}`;
}
