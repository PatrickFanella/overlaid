export type DashboardPanelPage = 'live' | 'bot' | 'config';

export type PanelLayoutItem = {
  id: string;
  title: string;
  visible: boolean;
  order: number;
};

export type PanelLayouts = Record<DashboardPanelPage, PanelLayoutItem[]>;

export const defaultPanelLayouts: PanelLayouts = {
  live: [
    { id: 'readiness', title: 'Readiness', visible: true, order: 1 },
    { id: 'preflight', title: 'Live preflight', visible: true, order: 2 },
    { id: 'eventsub', title: 'EventSub diagnostics', visible: true, order: 3 },
    { id: 'build-info', title: 'Build info', visible: true, order: 4 },
    { id: 'checklist', title: 'Checklist', visible: true, order: 5 },
    { id: 'session-log', title: 'Session log', visible: true, order: 6 },
    { id: 'event-history', title: 'Event history', visible: true, order: 7 },
  ],
  bot: [
    { id: 'commands', title: 'Commands', visible: true, order: 1 },
    { id: 'community', title: 'Community data', visible: true, order: 2 },
    { id: 'api-variables', title: 'API variables', visible: true, order: 3 },
    { id: 'timers', title: 'Timers', visible: true, order: 4 },
  ],
  config: [
    { id: 'dashboard-config', title: 'Shared dashboard config', visible: true, order: 1 },
    { id: 'obs-import', title: 'OBS import', visible: true, order: 2 },
    { id: 'overlay-noise', title: 'Overlay noise controls', visible: true, order: 3 },
    { id: 'automation-profiles', title: 'Automation profiles', visible: true, order: 4 },
    { id: 'mpris', title: 'MPRIS decision', visible: true, order: 5 },
  ],
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function normalizePageLayout(value: unknown, defaults: PanelLayoutItem[]) {
  const persisted = Array.isArray(value) ? value : [];
  const byId = new Map<string, Record<string, unknown>>();

  for (const item of persisted) {
    if (!isRecord(item) || typeof item.id !== 'string') continue;
    byId.set(item.id, item);
  }

  return defaults
    .map((fallback) => {
      const item = byId.get(fallback.id);

      return {
        ...fallback,
        visible: typeof item?.visible === 'boolean' ? item.visible : fallback.visible,
        order: typeof item?.order === 'number' && Number.isFinite(item.order) ? item.order : fallback.order,
      };
    })
    .sort((left, right) => left.order - right.order)
    .map((item, index) => ({ ...item, order: index + 1 }));
}

export function normalizePanelLayouts(value: unknown): PanelLayouts {
  const input = isRecord(value) ? value : {};

  return {
    live: normalizePageLayout(input.live, defaultPanelLayouts.live),
    bot: normalizePageLayout(input.bot, defaultPanelLayouts.bot),
    config: normalizePageLayout(input.config, defaultPanelLayouts.config),
  };
}

export function visiblePanelLayoutItems(items: PanelLayoutItem[]) {
  return items.filter((item) => item.visible).sort((left, right) => left.order - right.order);
}

export function movePanelLayoutItem(items: PanelLayoutItem[], id: string, direction: 'up' | 'down') {
  const ordered = [...items].sort((left, right) => left.order - right.order);
  const index = ordered.findIndex((item) => item.id === id);
  const targetIndex = direction === 'up' ? index - 1 : index + 1;

  if (index < 0 || targetIndex < 0 || targetIndex >= ordered.length) return ordered;

  const next = [...ordered];
  [next[index], next[targetIndex]] = [next[targetIndex], next[index]];

  return next.map((item, itemIndex) => ({ ...item, order: itemIndex + 1 }));
}

export function togglePanelLayoutItem(items: PanelLayoutItem[], id: string) {
  return items.map((item) => (item.id === id ? { ...item, visible: !item.visible } : item));
}
