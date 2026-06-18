import { useState, type ReactNode } from 'react';
import { fetchTwitchLoginURL } from '../../lib/api';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { loadOperatorSecret } from '../../lib/dashboardConfig';
import { dashboardRouteGroups, dashboardRoutes, getDashboardRoute } from '../../lib/dashboardRoutes';

type DashboardShellProps = {
  activePath: string;
  children: ReactNode;
};

export function DashboardShell({ activePath, children }: DashboardShellProps) {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const [loginState, setLoginState] = useState('');
  const activeRoute = getDashboardRoute(activePath) ?? dashboardRoutes[0];

  function startTwitchLogin() {
    setLoginState('opening');
    fetchTwitchLoginURL(apiBaseUrl, loadOperatorSecret())
      .then(({ authorizeUrl }) => {
        window.location.href = authorizeUrl;
      })
      .catch((error: unknown) => {
        setLoginState(error instanceof Error ? error.message : 'login failed');
      });
  }

  return (
    <main className="hud-shell hud-shell--dashboard min-h-screen text-zinc-100">
      <section className="hud-console-frame mx-auto max-w-[1200px]">
        <header className="hud-panel hud-panel--flat rounded-2xl p-4">
          <div className="hud-console-header lg:grid lg:grid-cols-[minmax(260px,0.8fr)_minmax(0,1.4fr)_auto] lg:items-start">
            <div className="min-w-0">
              <p className="hud-label">Broadcast ops console</p>
              <h1 className="hud-title hud-title--compact mt-1">{activeRoute.title}</h1>
              <p className="hud-copy mt-2 text-xs text-zinc-300">{activeRoute.description}</p>
            </div>

            <nav className="grid gap-2" aria-label="Dashboard sections">
              {dashboardRouteGroups.map((group) => {
                const routes = dashboardRoutes.filter((route) => route.intent === group.intent);

                return (
                  <div className="hud-route-group" key={group.intent}>
                    <p className="hud-kicker">{group.label}</p>
                    <div className="flex flex-wrap gap-1.5">
                      {routes.map((route) => (
                        <a
                          key={route.path}
                          className={`hud-button hud-button--small ${activePath === route.path ? 'border-zinc-100/40 bg-white/10 text-zinc-50' : 'text-zinc-400'}`}
                          href={route.path}
                          aria-current={activePath === route.path ? 'page' : undefined}
                        >
                          {route.navLabel}
                        </a>
                      ))}
                    </div>
                  </div>
                );
              })}
            </nav>

            <div className="flex flex-wrap items-center gap-2 lg:justify-end">
              <span className="hud-chip hud-chip--square hud-accent-emerald">{activeRoute.groupLabel}</span>
              {loginState ? <span className="hud-chip hud-chip--square hud-accent-amber max-w-[18rem] truncate">{loginState}</span> : null}
              <button className="hud-button hud-button--small hud-accent-fuchsia" type="button" onClick={startTwitchLogin}>
                Twitch login
              </button>
            </div>
          </div>
        </header>

        {children}
      </section>
    </main>
  );
}
