import { DashboardPage } from './routes/Dashboard';
import { HomePage } from './routes/Home';
import { OverlayAlertsPage } from './routes/OverlayAlerts';
import { OverlayBRBPage } from './routes/OverlayBRB';
import { OverlayChatPage } from './routes/OverlayChat';
import { OverlayDebugPage } from './routes/OverlayDebug';
import { OverlayGoalsPage } from './routes/OverlayGoals';
import { OverlayNowPlayingPage } from './routes/OverlayNowPlaying';
import { OverlayPlaceholderPage } from './routes/OverlayPlaceholder';
import { OverlayTickerPage } from './routes/OverlayTicker';

type AppProps = {
  pathname?: string;
};

const overlayRoutes = new Set([
  '/overlay/alerts',
  '/overlay/chat',
  '/overlay/goals',
  '/overlay/now-playing',
  '/overlay/ticker',
  '/overlay/brb',
  '/overlay/debug',
]);

function resolvePathname(pathname?: string) {
  if (pathname) {
    return normalizePathname(pathname);
  }

  if (typeof window === 'undefined') {
    return '/';
  }

  return normalizePathname(window.location.pathname);
}

function normalizePathname(pathname: string) {
  const cleaned = pathname.trim() || '/';
  if (cleaned === '/') {
    return '/';
  }

  return cleaned.replace(/\/+$/, '');
}

export function getAppRoute(pathname: string) {
  const route = normalizePathname(pathname);

  if (route === '/') {
    return <HomePage />;
  }

  if (route === '/dashboard') {
    return <DashboardPage />;
  }

  if (route === '/overlay/alerts') {
    return <OverlayAlertsPage />;
  }

  if (route === '/overlay/chat') {
    return <OverlayChatPage />;
  }

  if (route === '/overlay/goals') {
    return <OverlayGoalsPage />;
  }

  if (route === '/overlay/ticker') {
    return <OverlayTickerPage />;
  }

  if (route === '/overlay/now-playing') {
    return <OverlayNowPlayingPage />;
  }

  if (route === '/overlay/brb') {
    return <OverlayBRBPage />;
  }

  if (route === '/overlay/debug') {
    return <OverlayDebugPage />;
  }

  if (overlayRoutes.has(route)) {
    return <OverlayPlaceholderPage pathname={route} />;
  }

  return <HomePage pathname={route} />;
}

export default function App({ pathname }: AppProps = {}) {
  return getAppRoute(resolvePathname(pathname));
}
