import { useState, type ReactNode } from 'react';
import { fetchTwitchLoginURL } from '../../lib/api';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { loadOperatorSecret } from '../../lib/dashboardConfig';
import { dashboardRoutes } from '../../lib/dashboardRoutes';

type DashboardShellProps = {
  activePath: string;
  children: ReactNode;
};

export function DashboardShell({ activePath, children }: DashboardShellProps) {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const [loginState, setLoginState] = useState('');

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
    <main className="hud-shell hud-shell--dashboard hud-scanlines min-h-screen text-zinc-100">
      <section className="mx-auto flex max-w-[1180px] flex-col gap-3">
        <header className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
          <div className="grid gap-3 lg:grid-cols-[auto_1fr_auto] lg:items-center">
            <div className="min-w-0">
              <p className="hud-label">over-laid command center</p>
              <h1 className="hud-title hud-title--compact mt-1">Dashboard</h1>
            </div>

            <nav className="flex flex-wrap gap-1.5" aria-label="Dashboard sections">
              {dashboardRoutes.map((route) => (
                <a
                  key={route.path}
                  className={`hud-chip hud-chip--square ${activePath === route.path ? 'border-cyan-200/80 bg-cyan-300/15 text-cyan-50' : ''}`}
                  href={route.path}
                >
                  {route.navLabel}
                </a>
              ))}
            </nav>

            <div className="flex flex-wrap items-center gap-2 lg:justify-end">
              {loginState ? <span className="hud-chip hud-chip--square hud-accent-amber max-w-[18rem] truncate">{loginState}</span> : null}
              <button className="hud-action-button hud-action-button--compact hud-accent-fuchsia" type="button" onClick={startTwitchLogin}>
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
