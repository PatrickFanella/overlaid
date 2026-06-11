import { getDefaultApiBaseUrl } from './eventStream';

export type OverlayRoute = {
  path: string;
  title: string;
  description: string;
  defaultScene: 'main' | 'brb' | 'debug';
};

export const overlayRoutes: OverlayRoute[] = [
  { path: '/overlay/alerts', title: 'Alert feed', description: 'Live alert browser source', defaultScene: 'main' },
  { path: '/overlay/chat', title: 'Twitch chat', description: 'Live chat browser source', defaultScene: 'main' },
  { path: '/overlay/goals', title: 'Goal rail', description: 'Progress widget browser source', defaultScene: 'main' },
  { path: '/overlay/now-playing', title: 'Now playing', description: 'Music and media browser source', defaultScene: 'main' },
  { path: '/overlay/ticker', title: 'Ticker rail', description: 'Lower-third event ticker', defaultScene: 'main' },
  { path: '/overlay/brb', title: 'BRB overlay', description: 'Intermission scene control', defaultScene: 'brb' },
  { path: '/overlay/debug', title: 'Event inspector', description: 'SSE event stream debugger', defaultScene: 'debug' },
];

export function getBrowserOrigin() {
  if (typeof window !== 'undefined' && window.location.origin) {
    return window.location.origin;
  }

  return 'http://localhost:5173';
}

export function buildOverlaySourceUrl(path: string, apiBaseUrl: string) {
  const origin = getBrowserOrigin();
  const url = new URL(path, origin);
  const defaultApiBaseUrl = getDefaultApiBaseUrl(origin);

  if (apiBaseUrl && apiBaseUrl !== defaultApiBaseUrl) {
    url.searchParams.set('apiBaseUrl', apiBaseUrl);
  }

  return url.toString();
}
