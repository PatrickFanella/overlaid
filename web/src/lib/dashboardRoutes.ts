export type DashboardRoute = {
  path: string;
  navLabel: string;
  title: string;
  description: string;
  landmarkText: string;
  intent: 'monitor' | 'setup' | 'operate' | 'configure';
};

export const dashboardRoutes: DashboardRoute[] = [
  {
    path: '/dashboard',
    navLabel: 'Overview',
    title: 'Dashboard overview',
    description: 'Status, build identity, and route guide for choosing the right operator surface.',
    landmarkText: 'Ops route guide',
    intent: 'monitor',
  },
  {
    path: '/dashboard/live',
    navLabel: 'Live',
    title: 'Live ops cockpit',
    description: 'Recovery cockpit with readiness, runbook links, build info, EventSub diagnostics, checklist, session log, and event history.',
    landmarkText: 'Recovery console',
    intent: 'monitor',
  },
  {
    path: '/dashboard/setup',
    navLabel: 'Setup',
    title: 'OBS setup workflow',
    description: 'Setup checklist, browser-source URLs, and stream preset guidance.',
    landmarkText: 'Checklist',
    intent: 'setup',
  },
  {
    path: '/dashboard/sources',
    navLabel: 'Sources',
    title: 'Overlay source URLs',
    description: 'OBS browser-source URLs, active-profile preset view, and custom CSS copy helpers.',
    landmarkText: 'Stream presets',
    intent: 'setup',
  },
  {
    path: '/dashboard/control',
    navLabel: 'Stream control',
    title: 'Stream control',
    description: 'Operator quick actions, rehearsals, BRB controls, OBS controls, and session log.',
    landmarkText: 'Session log',
    intent: 'operate',
  },
  {
    path: '/dashboard/config',
    navLabel: 'Config',
    title: 'Operator config',
    description: 'Operator secret, automation profiles, Twitch interaction rules, MPRIS decision, and overlay noise controls.',
    landmarkText: 'Twitch interaction rules',
    intent: 'configure',
  },
];

export function getDashboardRoute(path: string) {
  return dashboardRoutes.find((route) => route.path === path);
}
