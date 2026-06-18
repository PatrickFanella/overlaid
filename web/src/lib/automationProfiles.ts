import type { AutomationProfile, DashboardConfig, QuickAction, StreamPreset } from './dashboardConfig';
import { normalizeDashboardConfig } from './dashboardConfig';
import { normalizePanelLayouts } from './dashboardLayout';

export type AutomationProfileWarning = { profileId: string; message: string };

function getProfileById(config: DashboardConfig, profileId: string) {
  return config.automationProfiles.find((profile) => profile.id === profileId);
}

export function getActiveAutomationProfile(config: DashboardConfig): AutomationProfile {
  return getProfileById(config, config.activeAutomationProfileId) ?? getProfileById(config, 'full-console') ?? config.automationProfiles[0] ?? {
    id: 'full-console',
    title: 'Full console',
    description: 'Show every quick action and stream preset.',
    quickActionIds: ['*'],
    streamPresetIds: ['*'],
  };
}

function selectByProfile<T extends { id: string }>(
  profileId: string,
  items: T[],
  selectedIds: string[],
  missingLabel: string,
  fallbackLabel: string,
): { items: T[]; warnings: AutomationProfileWarning[] } {
  const warnings: AutomationProfileWarning[] = [];

  if (selectedIds.some((id) => id.trim() === '*')) {
    return { items: [...items], warnings };
  }

  const byId = new Map(items.map((item) => [item.id, item]));
  const selected: T[] = [];
  const seen = new Set<string>();

  for (const rawId of selectedIds) {
    const id = rawId.trim();
    if (!id || seen.has(id)) continue;
    seen.add(id);

    const item = byId.get(id);
    if (!item) {
      warnings.push({ profileId, message: `Missing ${missingLabel}: ${id}` });
      continue;
    }

    selected.push(item);
  }

  if (selected.length === 0) {
    warnings.push({ profileId, message: `No valid ${fallbackLabel} selected; falling back to all ${fallbackLabel}.` });
    return { items: [...items], warnings };
  }

  return { items: selected, warnings };
}

export function selectQuickActionsForProfile(config: DashboardConfig): { actions: QuickAction[]; warnings: AutomationProfileWarning[] } {
  const profile = getActiveAutomationProfile(config);
  const { items, warnings } = selectByProfile(profile.id, config.quickActions, profile.quickActionIds, 'quick action', 'quick actions');

  return { actions: items, warnings };
}

export function selectStreamPresetsForProfile(config: DashboardConfig): { presets: StreamPreset[]; warnings: AutomationProfileWarning[] } {
  const profile = getActiveAutomationProfile(config);
  const { items, warnings } = selectByProfile(profile.id, config.streamPresets, profile.streamPresetIds, 'stream preset', 'stream presets');

  return { presets: items, warnings };
}

function serializeDashboardConfig(config: DashboardConfig): DashboardConfig {
  return {
    setupChecklist: [...config.setupChecklist],
    streamPresets: [...config.streamPresets],
    quickActions: [...config.quickActions],
    automationProfiles: config.automationProfiles.map((profile) => ({
      ...profile,
      quickActionIds: [...profile.quickActionIds],
      streamPresetIds: [...profile.streamPresetIds],
    })),
    activeAutomationProfileId: config.activeAutomationProfileId,
    panelLayouts: normalizePanelLayouts(config.panelLayouts),
    controlWidgets: [...config.controlWidgets],
    display: { ...config.display },
    customCSS: config.customCSS,
    mprisDecision: config.mprisDecision,
  };
}

export function exportDashboardConfig(config: DashboardConfig): string {
  return JSON.stringify(serializeDashboardConfig(normalizeDashboardConfig(config)));
}

export function importDashboardConfig(raw: string): DashboardConfig {
  try {
    const parsed = JSON.parse(raw) as Partial<DashboardConfig>;
    return normalizeDashboardConfig(parsed);
  } catch {
    return normalizeDashboardConfig({});
  }
}
