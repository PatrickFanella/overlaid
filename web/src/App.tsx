import { HomePage } from './routes/Home';
import { OverlayAlertsPage } from './routes/OverlayAlerts';
import { OverlayBRBPage } from './routes/OverlayBRB';
import { OverlayChatPage } from './routes/OverlayChat';
import { OverlayDebugPage } from './routes/OverlayDebug';
import { OverlayGoalsPage } from './routes/OverlayGoals';
import { OverlayNowPlayingPage } from './routes/OverlayNowPlaying';
import { OverlayPlaceholderPage } from './routes/OverlayPlaceholder';
import { OverlayTickerPage } from './routes/OverlayTicker';
import { DashboardConfigPage } from './routes/dashboard/Config';
import { DashboardControlPage } from './routes/dashboard/Control';
import { DashboardBotPage } from './routes/dashboard/Bot';
import { DashboardLivePage } from './routes/dashboard/Live';
import { DashboardOverviewPage } from './routes/dashboard/Overview';
import { DashboardSetupPage } from './routes/dashboard/Setup';
import { DashboardSourcesPage } from './routes/dashboard/Sources';
import { overlayRoutes as overlayRouteMetadata } from './lib/overlayRoutes';

type AppProps = {
  pathname?: string;
};

const overlayRoutePaths = new Set(overlayRouteMetadata.map((route) => route.path));

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
    return <DashboardOverviewPage />;
  }

  if (route === '/dashboard/setup') {
    return <DashboardSetupPage />;
  }

  if (route === '/dashboard/live') {
    return <DashboardLivePage />;
  }

  if (route === '/dashboard/sources') {
    return <DashboardSourcesPage />;
  }

  if (route === '/dashboard/control') {
    return <DashboardControlPage />;
  }

  if (route === '/dashboard/bot') {
    return <DashboardBotPage />;
  }

  if (route === '/dashboard/config') {
    return <DashboardConfigPage />;
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

  if (overlayRoutePaths.has(route)) {
    return <OverlayPlaceholderPage pathname={route} />;
  }

  return <HomePage pathname={route} />;
}

export default function App({ pathname }: AppProps = {}) {
  return getAppRoute(resolvePathname(pathname));
}
