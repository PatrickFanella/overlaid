import { getDefaultApiBaseUrl } from './eventStream';

export type OverlayRoute = {
  path: string;
  title: string;
  description: string;
  defaultScene: 'main' | 'brb' | 'starting' | 'ending' | 'camera' | 'debug';
  // Suggested OBS browser-source size.
  size: { width: number; height: number };
};

export const overlayRoutes: OverlayRoute[] = [
  { path: '/overlay/newsroom', title: 'Newsroom card', description: 'Current reaction link or audience question', defaultScene: 'main', size: { width: 1200, height: 300 } },
  { path: '/overlay/alerts', title: 'Alert feed', description: 'Follows, subs, bits, and homelab alerts on a purple card', defaultScene: 'main', size: { width: 1000, height: 720 } },
  { path: '/overlay/chat', title: 'Twitch chat', description: 'Chat column that fills from the bottom', defaultScene: 'main', size: { width: 600, height: 900 } },
  { path: '/overlay/goals', title: 'Goal rail', description: 'Primary goal with up to three secondary meters', defaultScene: 'main', size: { width: 780, height: 640 } },
  { path: '/overlay/now-playing', title: 'Now playing', description: 'MPRIS track strip with progress', defaultScene: 'main', size: { width: 920, height: 260 } },
  { path: '/overlay/ticker', title: 'Ticker rail', description: 'Full-width lower rail of recent events', defaultScene: 'main', size: { width: 1920, height: 72 } },
  { path: '/overlay/camera', title: 'Camera frame', description: 'Purple frame and nameplate to stack on a camera source', defaultScene: 'main', size: { width: 1920, height: 1080 } },
  { path: '/overlay/starting', title: 'Starting soon', description: 'Full-frame countdown card', defaultScene: 'starting', size: { width: 1920, height: 1080 } },
  { path: '/overlay/brb', title: 'BRB overlay', description: 'Full-frame intermission card driven from the dashboard', defaultScene: 'brb', size: { width: 1920, height: 1080 } },
  { path: '/overlay/ending', title: 'Stream ending', description: 'Full-frame end card', defaultScene: 'ending', size: { width: 1920, height: 1080 } },
  { path: '/overlay/debug', title: 'Event inspector', description: 'SSE event stream debugger for setup', defaultScene: 'debug', size: { width: 1000, height: 900 } },
];

export function getBrowserOrigin() {
  if (typeof window !== 'undefined' && window.location.origin) {
    return window.location.origin;
  }

  return 'http://localhost:5173';
}

export function buildOverlaySourceUrl(path: string, apiBaseUrl: string, key = '') {
  const origin = getBrowserOrigin();
  const url = new URL(path, origin);
  const defaultApiBaseUrl = getDefaultApiBaseUrl(origin);

  if (apiBaseUrl && apiBaseUrl !== defaultApiBaseUrl) {
    url.searchParams.set('apiBaseUrl', apiBaseUrl);
  }

  if (key) url.searchParams.set('key', key);
  return url.toString();
}
