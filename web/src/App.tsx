import type { ReactElement } from 'react';
import { DashboardAccountPage } from './routes/dashboard/Account';
import { LegalPage, PublicStatusPage } from './routes/Legal';
import { HomePage } from './routes/Home';
import { OverlayAlertsPage } from './routes/OverlayAlerts';
import { OverlayBRBPage } from './routes/OverlayBRB';
import { OverlayChatPage } from './routes/OverlayChat';
import { OverlayDebugPage } from './routes/OverlayDebug';
import { OverlayGoalsPage } from './routes/OverlayGoals';
import { OverlayNowPlayingPage } from './routes/OverlayNowPlaying';
import { OverlayCameraPage, OverlayEndingPage, OverlayStartingPage } from './routes/OverlayScenes';
import { OverlayTickerPage } from './routes/OverlayTicker';
import { DashboardChatPage } from './routes/dashboard/Chat';
import { DashboardHealthPage } from './routes/dashboard/Health';
import { DashboardOnAirPage } from './routes/dashboard/OnAir';
import { DashboardOverlaysPage } from './routes/dashboard/Overlays';
import { DashboardScenesPage } from './routes/dashboard/Scenes';
import { DashboardSessionsPage } from './routes/dashboard/Sessions';
import { DashboardSettingsPage } from './routes/dashboard/Settings';
import { resolveDashboardPath } from './lib/dashboardRoutes';

type AppProps = {
  pathname?: string;
};

const pages: Record<string, () => ReactElement> = {
  '/': () => <HomePage />,
  '/dashboard/account': () => <DashboardAccountPage />,
  '/privacy': () => <LegalPage kind="privacy" />,
  '/terms': () => <LegalPage kind="terms" />,
  '/status': () => <PublicStatusPage />,
  '/dashboard': () => <DashboardOnAirPage />,
  '/dashboard/scenes': () => <DashboardScenesPage />,
  '/dashboard/chat': () => <DashboardChatPage />,
  '/dashboard/overlays': () => <DashboardOverlaysPage />,
  '/dashboard/health': () => <DashboardHealthPage />,
  '/dashboard/settings': () => <DashboardSettingsPage />,
  '/dashboard/sessions': () => <DashboardSessionsPage />,
  '/overlay/alerts': () => <OverlayAlertsPage />,
  '/overlay/chat': () => <OverlayChatPage />,
  '/overlay/goals': () => <OverlayGoalsPage />,
  '/overlay/ticker': () => <OverlayTickerPage />,
  '/overlay/now-playing': () => <OverlayNowPlayingPage />,
  '/overlay/camera': () => <OverlayCameraPage />,
  '/overlay/starting': () => <OverlayStartingPage />,
  '/overlay/brb': () => <OverlayBRBPage />,
  '/overlay/ending': () => <OverlayEndingPage />,
  '/overlay/debug': () => <OverlayDebugPage />,
};

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
  const route = resolveDashboardPath(normalizePathname(pathname));
  const page = pages[route];
  return page ? page() : <HomePage pathname={route} />;
}

export default function App({ pathname }: AppProps = {}) {
  return getAppRoute(resolvePathname(pathname));
}
