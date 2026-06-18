export type DashboardRouteGroup = 'monitor' | 'setup' | 'operate' | 'configure';

export type DashboardRoute = {
  path: string;
  navLabel: string;
  title: string;
  description: string;
  landmarkText: string;
  intent: DashboardRouteGroup;
  groupLabel: 'Monitor' | 'Setup' | 'Operate' | 'Configure';
  priority: 'primary' | 'secondary';
};

export const dashboardRouteGroups: { label: DashboardRoute['groupLabel']; intent: DashboardRouteGroup }[] = [
  { label: 'Monitor', intent: 'monitor' },
  { label: 'Setup', intent: 'setup' },
  { label: 'Operate', intent: 'operate' },
  { label: 'Configure', intent: 'configure' },
];

export const dashboardRoutes: DashboardRoute[] = [
  {
    path: '/dashboard',
    navLabel: 'Overview',
    title: 'Dashboard overview',
    description: 'Status, build identity, and route guide for choosing the right operator surface.',
    landmarkText: 'Ops route guide',
    intent: 'monitor',
    groupLabel: 'Monitor',
    priority: 'primary',
  },
  {
    path: '/dashboard/live',
    navLabel: 'Live',
    title: 'Live ops cockpit',
    description: 'Recovery cockpit with readiness, runbook links, build info, EventSub diagnostics, checklist, session log, and event history.',
    landmarkText: 'Recovery console',
    intent: 'monitor',
    groupLabel: 'Monitor',
    priority: 'primary',
  },
  {
    path: '/dashboard/setup',
    navLabel: 'Setup',
    title: 'OBS setup workflow',
    description: 'Setup checklist, browser-source URLs, and stream preset guidance.',
    landmarkText: 'Checklist',
    intent: 'setup',
    groupLabel: 'Setup',
    priority: 'secondary',
  },
  {
    path: '/dashboard/sources',
    navLabel: 'Sources',
    title: 'Overlay source URLs',
    description: 'OBS browser-source URLs, active-profile preset view, and custom CSS copy helpers.',
    landmarkText: 'Stream presets',
    intent: 'setup',
    groupLabel: 'Setup',
    priority: 'secondary',
  },
  {
    path: '/dashboard/control',
    navLabel: 'Stream control',
    title: 'Stream control',
    description: 'Operator quick actions, rehearsals, BRB controls, OBS controls, and session log.',
    landmarkText: 'Session log',
    intent: 'operate',
    groupLabel: 'Operate',
    priority: 'primary',
  },
  {
    path: '/dashboard/bot',
    navLabel: 'Bot',
    title: 'Bot operations',
    description: 'Twitch bot audit, future custom commands, timers, counters, and moderation controls.',
    landmarkText: 'Bot audit',
    intent: 'operate',
    groupLabel: 'Operate',
    priority: 'primary',
  },
  {
    path: '/dashboard/config',
    navLabel: 'Config',
    title: 'Operator config',
    description: 'Operator secret, automation profiles, Twitch interaction rules, MPRIS decision, and overlay noise controls.',
    landmarkText: 'Twitch interaction rules',
    intent: 'configure',
    groupLabel: 'Configure',
    priority: 'primary',
  },
];

export function getDashboardRoute(path: string) {
  return dashboardRoutes.find((route) => route.path === path);
}
