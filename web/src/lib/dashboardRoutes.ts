// Dashboard navigation, grouped by what the operator is doing rather than by
// subsystem. `landmarkText` is a string each page must render; route tests use
// it to prove the right page mounted.

export type DashboardRouteGroup = 'show' | 'prepare' | 'review';

export type DashboardRoute = {
  path: string;
  navLabel: string;
  title: string;
  description: string;
  landmarkText: string;
  intent: DashboardRouteGroup;
  shortcut: string;
};

export const dashboardRouteGroups: { label: string; intent: DashboardRouteGroup }[] = [
  { label: 'Show', intent: 'show' },
  { label: 'Prepare', intent: 'prepare' },
  { label: 'Review', intent: 'review' },
];

export const dashboardRoutes: DashboardRoute[] = [

  {
    path: '/dashboard',
    navLabel: 'On air',
    title: 'On air',
    description: 'Session tally, quick actions, BRB, preflight, and the latest events in one screen.',
    landmarkText: 'Run configured actions',
    intent: 'show',
    shortcut: '1',
  },
  {
    path: '/dashboard/newsroom', navLabel: 'Newsroom', title: 'Newsroom', description: 'Reaction deck, moderator triage, shelves, show notes, and media enrichment.', landmarkText: 'Moderator queue', intent: 'show', shortcut: '8',
  },
  {
    path: '/dashboard/scenes',
    navLabel: 'Scenes',
    title: 'Scenes and actions',
    description: 'OBS scenes, inputs and items, quick-action presets, sample events, and rehearsal runs.',
    landmarkText: 'OBS controls',
    intent: 'show',
    shortcut: '2',
  },
  {
    path: '/dashboard/chat',
    navLabel: 'Chat',
    title: 'Chat and bot',
    description: 'Bot commands, timers, counters, quotes, moderation, Twitch rules, and the reward queue.',
    landmarkText: 'Bot commands',
    intent: 'show',
    shortcut: '3',
  },
  {
    path: '/dashboard/overlays',
    navLabel: 'Overlays',
    title: 'Overlays',
    description: 'Live previews and OBS browser-source URLs for every overlay, plus presets, custom CSS, and the setup checklist.',
    landmarkText: 'Browser sources',
    intent: 'prepare',
    shortcut: '4',
  },
  {
    path: '/dashboard/automations',
    navLabel: 'Automations',
    title: 'Automations',
    description: 'Discover, inspect and publish immutable configuration versions without executing actions.',
    landmarkText: 'Automation versions',
    intent: 'prepare',
    shortcut: '9',
  },
  {
    path: '/dashboard/health',
    navLabel: 'Health',
    title: 'Health',
    description: 'Backend readiness, EventSub, build identity, and recovery runbooks.',
    landmarkText: 'Backend readiness',
    intent: 'prepare',
    shortcut: '5',
  },
  {
    path: '/dashboard/settings',
    navLabel: 'Settings',
    title: 'Settings',
    description: 'Operator secret, shared config, OBS import, overlay limits, automation profiles, and MPRIS.',
    landmarkText: 'Operator secret',
    intent: 'prepare',
    shortcut: '6',
  },
  {
    path: '/dashboard/sessions',
    navLabel: 'Sessions',
    title: 'Sessions',
    description: 'Stream and rehearsal sessions, reports, the operator action log, and persisted event history.',
    landmarkText: 'Rehearsal audit',
    intent: 'review',
    shortcut: '7',
  },
];

// Paths from the previous console layout. They keep working so OBS docks and
// bookmarks do not break.
export const legacyDashboardPaths: Record<string, string> = {
  '/dashboard/live': '/dashboard/health',
  '/dashboard/control': '/dashboard/scenes',
  '/dashboard/setup': '/dashboard/overlays',
  '/dashboard/sources': '/dashboard/overlays',
  '/dashboard/bot': '/dashboard/chat',
  '/dashboard/config': '/dashboard/settings',
};

export function resolveDashboardPath(path: string) {
  return legacyDashboardPaths[path] ?? path;
}

export function getDashboardRoute(path: string) {
  const resolved = resolveDashboardPath(path);
  return dashboardRoutes.find((route) => route.path === resolved);
}
