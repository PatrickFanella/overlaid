import { defaultPanelLayouts, normalizePanelLayouts, type PanelLayouts } from './dashboardLayout';

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

export type AutomationProfile = {
  id: string;
  title: string;
  description: string;
  quickActionIds: string[];
  streamPresetIds: string[];
};

export type ControlWidget = {
  id: string;
  title: string;
  enabled: boolean;
  order: number;
  compact: boolean;
};

export type DisplayConfig = {
  alertMaxVisible: number;
  alertDedupeSeconds: number;
  chatMaxVisible: number;
  tickerMaxItems: number;
};

export type QuickAction =
  | { id: string; label: string; kind: 'sequence'; actionIds: string[]; stopOnError: boolean; confirm?: boolean }
  | { id: string; label: string; kind: 'setScene'; sceneName: string; confirm?: boolean }
  | { id: string; label: string; kind: 'runPreset'; presetId: string; sceneName: string; confirm?: boolean }
  | { id: string; label: string; kind: 'refreshSource'; inputName: string; confirm?: boolean }
  | { id: string; label: string; kind: 'sendAlert'; title: string; message: string; severity: 'info' | 'success' | 'warning' | 'error'; confirm?: boolean }
  | { id: string; label: string; kind: 'showBRB'; title: string; message: string; eta: string; items: string[]; confirm?: boolean }
  | { id: string; label: string; kind: 'hideBRB'; confirm?: boolean }
  | { id: string; label: string; kind: 'clearOverlays'; confirm?: boolean }
  | { id: string; label: string; kind: 'muteInput'; inputName: string; muted: boolean; confirm?: boolean };

export const quickActionKinds = ['sequence', 'setScene', 'sendAlert', 'showBRB', 'hideBRB', 'clearOverlays', 'muteInput', 'runPreset', 'refreshSource'] as const;

export type DashboardConfig = {
  setupChecklist: SetupChecklistItem[];
  streamPresets: StreamPreset[];
  quickActions: QuickAction[];
  automationProfiles: AutomationProfile[];
  activeAutomationProfileId: string;
  panelLayouts: PanelLayouts;
  controlWidgets: ControlWidget[];
  display: DisplayConfig;
  customCSS: string;
  mprisDecision: 'enabled' | 'disabled' | 'undecided';
};

const CONFIG_KEY = 'overlaid.dashboard.config.v1';
const OPERATOR_SECRET_KEY = 'overlaid.operatorSecret.session.v1';

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value);
}

function sanitizeStringArray(value: unknown, fallback: string[]) {
  if (!isStringArray(value)) return [...fallback];

  const sanitized = value.filter((item): item is string => typeof item === 'string' && item.trim().length > 0);
  return sanitized.length > 0 ? sanitized : [...fallback];
}

function sanitizeBoolean(value: unknown, fallback: boolean) {
  return typeof value === 'boolean' ? value : fallback;
}

function sanitizeNumber(value: unknown, fallback: number) {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback;
}

function sanitizeSetupChecklistItem(item: unknown, fallback: SetupChecklistItem): SetupChecklistItem | null {
  if (!isRecord(item) || typeof item.id !== 'string') return null;

  return {
    id: item.id,
    title: typeof item.title === 'string' ? item.title : fallback.title,
    description: typeof item.description === 'string' ? item.description : fallback.description,
    completed: sanitizeBoolean(item.completed, fallback.completed),
  };
}

function sanitizeStreamPreset(preset: unknown, fallback: StreamPreset): StreamPreset | null {
  if (!isRecord(preset) || typeof preset.id !== 'string') return null;

  return {
    id: preset.id,
    title: typeof preset.title === 'string' ? preset.title : fallback.title,
    sources: sanitizeStringArray(preset.sources, fallback.sources),
  };
}

function sanitizeControlWidget(widget: unknown, fallback: ControlWidget): ControlWidget | null {
  if (!isRecord(widget) || typeof widget.id !== 'string') return null;

  return {
    id: widget.id,
    title: typeof widget.title === 'string' ? widget.title : fallback.title,
    enabled: sanitizeBoolean(widget.enabled, fallback.enabled),
    order: sanitizeNumber(widget.order, fallback.order),
    compact: sanitizeBoolean(widget.compact, fallback.compact),
  };
}

export function sanitizeDisplayConfig(display: unknown): DisplayConfig {
  const fallback = defaultDashboardConfig.display;
  if (!isRecord(display)) return { ...fallback };

  return {
    alertMaxVisible: sanitizeNumber(display.alertMaxVisible, fallback.alertMaxVisible),
    alertDedupeSeconds: sanitizeNumber(display.alertDedupeSeconds, fallback.alertDedupeSeconds),
    chatMaxVisible: sanitizeNumber(display.chatMaxVisible, fallback.chatMaxVisible),
    tickerMaxItems: sanitizeNumber(display.tickerMaxItems, fallback.tickerMaxItems),
  };
}

function sanitizeQuickAction(action: unknown): QuickAction | null {
  if (!isRecord(action) || typeof action.id !== 'string' || typeof action.label !== 'string' || typeof action.kind !== 'string') {
    return null;
  }

  const baseId = action.id;
  const baseLabel = action.label;

  switch (action.kind) {
    case 'sequence': {
      return {
        id: baseId,
        label: baseLabel,
        kind: 'sequence',
        actionIds: sanitizeStringArray(action.actionIds, ['main-scene', 'refresh-alerts']),
        stopOnError: sanitizeBoolean(action.stopOnError, baseId === 'sequence-go-live-main' ? false : true),
        ...(typeof action.confirm === 'boolean' ? { confirm: action.confirm } : {}),
      };
    }
    case 'setScene':
      return {
        id: baseId,
        label: baseLabel,
        kind: 'setScene',
        sceneName: typeof action.sceneName === 'string' ? action.sceneName : '40_MAIN',
        ...(typeof action.confirm === 'boolean' ? { confirm: action.confirm } : {}),
      };
    case 'runPreset':
      return {
        id: baseId,
        label: baseLabel,
        kind: 'runPreset',
        presetId: typeof action.presetId === 'string' ? action.presetId : 'main',
        sceneName: typeof action.sceneName === 'string' ? action.sceneName : '40_MAIN',
        ...(typeof action.confirm === 'boolean' ? { confirm: action.confirm } : {}),
      };
    case 'refreshSource':
      return {
        id: baseId,
        label: baseLabel,
        kind: 'refreshSource',
        inputName: typeof action.inputName === 'string' ? action.inputName : 'ALERTS',
        ...(typeof action.confirm === 'boolean' ? { confirm: action.confirm } : {}),
      };
    case 'sendAlert':
      return {
        id: baseId,
        label: baseLabel,
        kind: 'sendAlert',
        title: typeof action.title === 'string' ? action.title : 'Sample alert',
        message: typeof action.message === 'string' ? action.message : 'Quick action fired',
        severity:
          action.severity === 'info' || action.severity === 'success' || action.severity === 'warning' || action.severity === 'error'
            ? action.severity
            : 'success',
        ...(typeof action.confirm === 'boolean' ? { confirm: action.confirm } : {}),
      };
    case 'showBRB':
      return {
        id: baseId,
        label: baseLabel,
        kind: 'showBRB',
        title: typeof action.title === 'string' ? action.title : 'Be right back',
        message: typeof action.message === 'string' ? action.message : 'Short intermission. Stream resumes soon.',
        eta: typeof action.eta === 'string' ? action.eta : '5 min',
        items: sanitizeStringArray(action.items, ['Hydrate', 'Stretch', 'Chat stays live']),
        ...(typeof action.confirm === 'boolean' ? { confirm: action.confirm } : {}),
      };
    case 'hideBRB':
      return {
        id: baseId,
        label: baseLabel,
        kind: 'hideBRB',
        ...(typeof action.confirm === 'boolean' ? { confirm: action.confirm } : {}),
      };
    case 'clearOverlays':
      return {
        id: baseId,
        label: baseLabel,
        kind: 'clearOverlays',
        ...(typeof action.confirm === 'boolean' ? { confirm: action.confirm } : {}),
      };
    case 'muteInput':
      return {
        id: baseId,
        label: baseLabel,
        kind: 'muteInput',
        inputName: typeof action.inputName === 'string' ? action.inputName : 'Microphone',
        muted: sanitizeBoolean(action.muted, true),
        ...(typeof action.confirm === 'boolean' ? { confirm: action.confirm } : {}),
      };
    default:
      return null;
  }
}

function sanitizeAutomationProfile(profile: unknown, fallback: AutomationProfile): AutomationProfile | null {
  if (!isRecord(profile) || typeof profile.id !== 'string') return null;

  return {
    id: profile.id,
    title: typeof profile.title === 'string' ? profile.title : fallback.title,
    description: typeof profile.description === 'string' ? profile.description : fallback.description,
    quickActionIds: sanitizeStringArray(profile.quickActionIds, fallback.quickActionIds),
    streamPresetIds: sanitizeStringArray(profile.streamPresetIds, fallback.streamPresetIds),
  };
}

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
  automationProfiles: [
    {
      id: 'full-console',
      title: 'Full console',
      description: 'Show every quick action and stream preset.',
      quickActionIds: ['*'],
      streamPresetIds: ['*'],
    },
    {
      id: 'main-live',
      title: 'Main live',
      description: 'Primary live scene controls and alert refresh flow.',
      quickActionIds: ['sequence-go-live-main', 'preset-main', 'refresh-alerts', 'sample-alert', 'hide-brb'],
      streamPresetIds: ['main', 'debug'],
    },
    {
      id: 'brb-ops',
      title: 'BRB ops',
      description: 'Intermission scene controls and BRB overlay state.',
      quickActionIds: ['sequence-start-brb', 'brb-scene', 'show-brb', 'hide-brb', 'refresh-alerts'],
      streamPresetIds: ['brb', 'debug'],
    },
  ],
  activeAutomationProfileId: 'full-console',
  panelLayouts: defaultPanelLayouts,
  quickActions: [
    { id: 'main-scene', label: 'Go Main', kind: 'setScene', sceneName: '40_MAIN' },
    { id: 'preset-main', label: 'Run Main preset', kind: 'runPreset', presetId: 'main', sceneName: '40_MAIN', confirm: true },
    { id: 'refresh-alerts', label: 'Refresh Alerts', kind: 'refreshSource', inputName: 'ALERTS' },
    { id: 'brb-scene', label: 'Go Stand By', kind: 'setScene', sceneName: '30_STAND_BY', confirm: true },
    { id: 'sample-alert', label: 'Sample Alert', kind: 'sendAlert', title: 'Sample alert', message: 'Quick action fired', severity: 'success' },
    { id: 'show-brb', label: 'Show BRB', kind: 'showBRB', title: 'Be right back', message: 'Short intermission. Stream resumes soon.', eta: '5 min', items: ['Hydrate', 'Stretch', 'Chat stays live'] },
    { id: 'hide-brb', label: 'Hide BRB', kind: 'hideBRB', confirm: false },
    { id: 'sequence-go-live-main', label: 'Go Live Main', kind: 'sequence', actionIds: ['main-scene', 'refresh-alerts', 'hide-brb'], stopOnError: false, confirm: true },
    { id: 'sequence-start-brb', label: 'Start BRB', kind: 'sequence', actionIds: ['brb-scene', 'show-brb'], stopOnError: true, confirm: true },
  ],
  controlWidgets: [
    { id: 'quick-actions', title: 'Quick actions', enabled: true, order: 1, compact: true },
    { id: 'sample-events', title: 'Sample events', enabled: true, order: 2, compact: true },
    { id: 'brb', title: 'BRB controls', enabled: true, order: 3, compact: false },
    { id: 'obs', title: 'OBS controls', enabled: true, order: 4, compact: false },
    { id: 'editor', title: 'Quick action editor', enabled: true, order: 5, compact: false },
  ],
  display: { alertMaxVisible: 3, alertDedupeSeconds: 8, chatMaxVisible: 8, tickerMaxItems: 8 },
  customCSS: `body { background: transparent !important; }\n\n/* Compact OBS crop helper */\n#root { overflow: hidden; }`,
  mprisDecision: 'undecided',
};

function mergeQuickActions(persistedQuickActions: QuickAction[] | undefined) {
  const mergedQuickActions: QuickAction[] = [];
  const seenIds = new Set<string>();

  for (const action of persistedQuickActions ?? []) {
    if (seenIds.has(action.id)) continue;
    seenIds.add(action.id);
    mergedQuickActions.push(migrateBuiltInQuickAction(action));
  }

  for (const action of defaultDashboardConfig.quickActions) {
    if (seenIds.has(action.id)) continue;
    seenIds.add(action.id);
    mergedQuickActions.push(action);
  }

  return mergedQuickActions;
}

function migrateBuiltInQuickAction(action: QuickAction): QuickAction {
  if ((action.id === 'main-scene' || action.id === 'preset-main') && (action.kind === 'setScene' || action.kind === 'runPreset') && action.sceneName === 'Main') {
    return { ...action, sceneName: '40_MAIN' };
  }
  if (action.id === 'brb-scene' && action.kind === 'setScene' && action.sceneName === 'BRB') {
    return { ...action, label: action.label === 'Go BRB' ? 'Go Stand By' : action.label, sceneName: '30_STAND_BY' };
  }
  if (action.id === 'refresh-alerts' && action.kind === 'refreshSource' && action.inputName === 'Alerts Browser') {
    return { ...action, inputName: 'ALERTS' };
  }
  if (action.id === 'sequence-go-live-main' && action.kind === 'sequence' && action.stopOnError) {
    return { ...action, stopOnError: false };
  }
  return action;
}

function mergeAutomationProfiles(persistedAutomationProfiles: AutomationProfile[] | undefined) {
  const mergedProfiles: AutomationProfile[] = [];
  const seenIds = new Set<string>();

  for (const profile of persistedAutomationProfiles ?? []) {
    if (seenIds.has(profile.id)) continue;
    seenIds.add(profile.id);
    mergedProfiles.push({
      ...profile,
      quickActionIds: [...profile.quickActionIds],
      streamPresetIds: [...profile.streamPresetIds],
    });
  }

  for (const profile of defaultDashboardConfig.automationProfiles) {
    if (seenIds.has(profile.id)) continue;
    seenIds.add(profile.id);
    mergedProfiles.push({
      ...profile,
      quickActionIds: [...profile.quickActionIds],
      streamPresetIds: [...profile.streamPresetIds],
    });
  }

  return mergedProfiles;
}

export function normalizeDashboardConfig(parsed: Partial<DashboardConfig>): DashboardConfig {
  const persistedQuickActions = Array.isArray(parsed.quickActions)
    ? parsed.quickActions.map((action) => sanitizeQuickAction(action)).filter((action): action is QuickAction => action !== null)
    : undefined;
  const persistedAutomationProfiles = Array.isArray(parsed.automationProfiles)
    ? parsed.automationProfiles
        .map((profile) => sanitizeAutomationProfile(profile, defaultDashboardConfig.automationProfiles.find((item) => item.id === (isRecord(profile) ? profile.id : undefined)) ?? defaultDashboardConfig.automationProfiles[0]))
        .filter((profile): profile is AutomationProfile => profile !== null)
    : undefined;
  const mergedProfiles = mergeAutomationProfiles(persistedAutomationProfiles);
  const activeAutomationProfileId =
    typeof parsed.activeAutomationProfileId === 'string' && mergedProfiles.some((profile) => profile.id === parsed.activeAutomationProfileId)
      ? parsed.activeAutomationProfileId
      : 'full-console';

  return {
    setupChecklist: Array.isArray(parsed.setupChecklist)
      ? parsed.setupChecklist
          .map((item, index) => sanitizeSetupChecklistItem(item, defaultDashboardConfig.setupChecklist[index] ?? defaultDashboardConfig.setupChecklist[0]))
          .filter((item): item is SetupChecklistItem => item !== null)
      : [...defaultDashboardConfig.setupChecklist],
    streamPresets: Array.isArray(parsed.streamPresets)
      ? parsed.streamPresets
          .map((preset, index) => sanitizeStreamPreset(preset, defaultDashboardConfig.streamPresets[index] ?? defaultDashboardConfig.streamPresets[0]))
          .filter((preset): preset is StreamPreset => preset !== null)
      : [...defaultDashboardConfig.streamPresets],
    quickActions: mergeQuickActions(persistedQuickActions),
    automationProfiles: mergedProfiles,
    activeAutomationProfileId,
    panelLayouts: normalizePanelLayouts(parsed.panelLayouts),
    controlWidgets: Array.isArray(parsed.controlWidgets)
      ? parsed.controlWidgets
          .map((widget, index) => sanitizeControlWidget(widget, defaultDashboardConfig.controlWidgets[index] ?? defaultDashboardConfig.controlWidgets[0]))
          .filter((widget): widget is ControlWidget => widget !== null)
      : [...defaultDashboardConfig.controlWidgets],
    display: sanitizeDisplayConfig(parsed.display),
    customCSS: typeof parsed.customCSS === 'string' ? parsed.customCSS : defaultDashboardConfig.customCSS,
    mprisDecision:
      parsed.mprisDecision === 'enabled' || parsed.mprisDecision === 'disabled' ? parsed.mprisDecision : defaultDashboardConfig.mprisDecision,
  };
}

export function loadDashboardConfig(): DashboardConfig {
  if (typeof window === 'undefined') return normalizeDashboardConfig({});
  const raw = window.localStorage.getItem(CONFIG_KEY);
  if (!raw) return normalizeDashboardConfig({});

  try {
    const parsed = JSON.parse(raw) as Partial<DashboardConfig>;
    return normalizeDashboardConfig(parsed);
  } catch {
    return normalizeDashboardConfig({});
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
    case 'sequence':
      return { id, label, kind, actionIds: ['main-scene', 'refresh-alerts'], stopOnError: true, confirm: true };
    case 'setScene':
      return { id, label, kind, sceneName: '40_MAIN' };
    case 'runPreset':
      return { id, label, kind, presetId: 'main', sceneName: '40_MAIN', confirm: true };
    case 'refreshSource':
      return { id, label, kind, inputName: 'ALERTS' };
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
