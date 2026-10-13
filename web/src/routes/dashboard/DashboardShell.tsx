import { useEffect, useState, useSyncExternalStore, type ReactNode } from 'react';
import { fetchTwitchLoginURL } from '../../lib/api';
import { dashboardRouteGroups, dashboardRoutes, getDashboardRoute, resolveDashboardPath } from '../../lib/dashboardRoutes';
import { DeskStatusProvider, eventSubLamp, obsLamp, onAirState, readinessLamp, twitchLamp, useDeskStatus } from './deskStatus';
import { Lamp } from './ui';
import { AccountProvider, loginURL, useAccount } from './accountSession';

type DashboardShellProps = {
  activePath: string;
  children: ReactNode;
};

export function DashboardShell({ activePath, children }: DashboardShellProps) {
  return (
    <AccountProvider><AccountGate activePath={activePath}><DeskStatusProvider>
      <DeskFrame activePath={resolveDashboardPath(activePath)}>{children}</DeskFrame>
    </DeskStatusProvider></AccountGate></AccountProvider>
  );
}

function AccountGate({ activePath, children }: { activePath: string; children: ReactNode }) {
  const { account, loading } = useAccount();
  const client = useSyncExternalStore(() => () => {}, () => true, () => false);
  if (loading && client) return <div className="desk"><main className="desk-page"><p role="status">Loading account…</p></main></div>;
  if (account.deploymentMode === 'hosted' && activePath !== '/dashboard/account' && (!account.authenticated || !account.activeTenantId)) {
    return <div className="desk"><main className="desk-page"><h1 className="desk-title">Your workspace</h1><p>{account.authenticated ? 'Select or create a workspace to open the operator desk.' : 'Sign in to open your workspaces.'}</p><a className="desk-btn" href={account.authenticated ? '/dashboard/account' : loginURL()}>{account.authenticated ? 'Manage workspaces' : 'Sign in with Twitch'}</a><p><a href="/privacy">Privacy policy</a> · <a href="/terms">Terms of service</a></p></main></div>;
  }
  return children;
}

function DeskFrame({ activePath, children }: DashboardShellProps) {
  const activeRoute = getDashboardRoute(activePath) ?? dashboardRoutes[0];
  const title = activePath === '/dashboard/account' ? 'Account and workspace' : activeRoute.navLabel;

  useLegacyPathRewrite(activePath);
  useRouteShortcuts();

  useEffect(() => {
    document.title = `${title} · over-laid`;
  }, [title]);

  return (
    <div className="desk">
      <nav className="desk-nav" aria-label="Dashboard sections">
        <a className="desk-wordmark" href="/dashboard">
          over-laid <small>operator desk</small>
        </a>

        {dashboardRouteGroups.map((group) => (
          <div className="desk-nav__group" key={group.intent}>
            <p className="desk-nav__heading">{group.label}</p>
            {dashboardRoutes
              .filter((route) => route.intent === group.intent)
              .map((route) => (
                <a key={route.path} className="desk-nav__link" href={route.path} aria-current={activePath === route.path ? 'page' : undefined} title={`${route.description} (Alt+${route.shortcut})`}>
                  {route.navLabel}
                  <span className="desk-nav__key">⌥{route.shortcut}</span>
                </a>
              ))}
          </div>
        ))}

        <div className="desk-nav__foot">
          <a className="desk-nav__link" href="/dashboard/account">Account and workspace</a>
          <a className="desk-nav__link" href="/privacy">Privacy</a>
          <a className="desk-nav__link" href="/terms">Terms</a>
          <a className="desk-nav__link" href="/">
            All routes
          </a>
          <a className="desk-nav__link" href="/overlay/debug" target="_blank" rel="noreferrer">
            Event inspector
          </a>
        </div>
      </nav>

      <div className="desk-main">
        <TopBar />
        <main className="desk-page">{children}</main>
      </div>
    </div>
  );
}

function TopBar() {
  const status = useDeskStatus();
  const { account } = useAccount();
  const [loginState, setLoginState] = useState('');
  const state = onAirState(status.session);
  const twitch = twitchLamp(status.twitch);
  const eventSub = eventSubLamp(status.twitch);
  const obs = obsLamp(status.obs);
  const readiness = readinessLamp(status.readiness);

  function startTwitchLogin() {
    setLoginState('Opening Twitch…');
    fetchTwitchLoginURL(status.apiBaseUrl, status.operatorSecret)
      .then(({ authorizeUrl }) => {
        window.location.href = authorizeUrl;
      })
      .catch((error: unknown) => {
        setLoginState(error instanceof Error ? error.message : 'Twitch login failed');
      });
  }

  return (
    <header className="desk-top">
      <div className="desk-top__lamps" role="status" aria-label="System status">
        <span className={`desk-tally desk-tally--${state}`}>{state === 'live' ? 'On air' : state === 'rehearsal' ? 'Rehearsal' : 'Off air'}</span>
        <Lamp label="OBS" tone={obs.tone} value={obs.value} busy={status.loading} />
        <Lamp label="Twitch" tone={twitch.tone} value={twitch.value} busy={status.loading} />
        <Lamp label="EventSub" tone={eventSub.tone} value={eventSub.value} busy={status.loading} />
        <Lamp label="Ready" tone={readiness.tone} value={readiness.value} busy={status.loading} />
        <Lamp label={account.deploymentMode === 'hosted' ? 'Account' : 'Operator'} tone={account.deploymentMode === 'hosted' ? 'ok' : status.operatorSecret ? 'ok' : 'bad'} value={account.deploymentMode === 'hosted' ? account.tenants.find((item) => item.id === account.activeTenantId)?.name || 'signed in' : status.operatorSecret ? 'secret loaded' : 'secret missing'} />
      </div>

      <div className="desk-top__actions">
        {loginState ? <span className="desk-tag desk-tag--warn max-w-72 truncate">{loginState}</span> : null}
        <button className="desk-btn desk-btn--small desk-btn--ghost" type="button" disabled={status.loading} onClick={status.refresh}>
          {status.loading ? 'Checking…' : 'Recheck'}
        </button>
        <button className="desk-btn desk-btn--small" type="button" onClick={startTwitchLogin}>
          Twitch login
        </button>
      </div>
    </header>
  );
}

// Old console paths render the new page; swap the address bar to match.
function useLegacyPathRewrite(activePath: string) {
  useEffect(() => {
    if (window.location.pathname.replace(/\/+$/, '') !== activePath) {
      window.history.replaceState(null, '', `${activePath}${window.location.search}${window.location.hash}`);
    }
  }, [activePath]);
}

// Alt+1…7 jumps between pages. Alt avoids clashing with typing in fields.
function useRouteShortcuts() {
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (!event.altKey || event.ctrlKey || event.metaKey) return;
      const route = dashboardRoutes.find((item) => event.code === `Digit${item.shortcut}`);
      if (route) {
        event.preventDefault();
        window.location.href = route.path;
      }
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);
}
