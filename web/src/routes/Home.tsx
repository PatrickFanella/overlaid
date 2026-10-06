import { getOverlayApiBaseUrl } from '../lib/eventStream';
import { dashboardRoutes } from '../lib/dashboardRoutes';
import { overlayRoutes } from '../lib/overlayRoutes';

type HomePageProps = {
  pathname?: string;
};

export function HomePage({ pathname }: HomePageProps = {}) {
  const apiBaseUrl = getOverlayApiBaseUrl();

  return (
    <div className="desk desk--home">
      <main className="desk-home">
        <header className="desk-page__head">
          <div>
            <p className="desk-panel__kicker">over-laid</p>
            <h1 className="desk-page__title">Overlay routes</h1>
            <p className="desk-page__lede">
              OBS browser sources and the operator desk for <strong>patrick__eff</strong>. Twitch OAuth works for any channel.
            </p>
          </div>
          <div className="desk-panel__actions">
            <a className="desk-btn desk-btn--small" href={`${apiBaseUrl}/api/auth/twitch/login`}>
              Twitch login
            </a>
            <a className="desk-btn desk-btn--small desk-btn--ghost" href={`${apiBaseUrl}/api/auth/twitch/status`}>
              Twitch status JSON
            </a>
          </div>
        </header>

        {pathname ? (
          <div className="desk-notice desk-notice--warn">
            Nothing lives at <code>{pathname}</code>; pick a route below.
          </div>
        ) : null}

        <RouteList title="Operator desk" routes={dashboardRoutes.map((route) => ({ path: route.path, title: route.title, description: route.description }))} />
        <RouteList title="Browser sources" routes={overlayRoutes.map((route) => ({ path: route.path, title: route.title, description: `${route.description} · ${route.size.width}×${route.size.height}` }))} />
      </main>
    </div>
  );
}

function RouteList({ title, routes }: { title: string; routes: { path: string; title: string; description: string }[] }) {
  return (
    <section className="grid gap-3" aria-label={title}>
      <h2 className="desk-nav__heading">{title}</h2>
      <ul className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
        {routes.map((route) => (
          <li key={route.path}>
            <a className="desk-link-card" href={route.path}>
              <span className="desk-row__meta mono">{route.path}</span>
              <span className="desk-row__title">{route.title}</span>
              <span className="desk-row__text">{route.description}</span>
            </a>
          </li>
        ))}
      </ul>
    </section>
  );
}
