import { getOverlayApiBaseUrl } from '../lib/eventStream';

const routes = [
  ['/dashboard', 'Command center', 'Dashboard for Twitch auth, test alerts, clear events, and webhook checks.'],
  ['/overlay/alerts', 'Live alert overlay', 'Fully functional SSE-driven alert browser source.'],
  ['/overlay/chat', 'Chat overlay', 'Live Twitch chat browser source.'],
  ['/overlay/goals', 'Goals overlay', 'Placeholder route for progress widgets.'],
  ['/overlay/now-playing', 'Now playing', 'Live MPRIS music/media browser source.'],
  ['/overlay/ticker', 'Ticker overlay', 'Placeholder route for rotating status cards.'],
  ['/overlay/brb', 'BRB overlay', 'Placeholder route for intermission scenes.'],
  ['/overlay/debug', 'Debug overlay', 'Event inspector for development and QA.'],
] as const;

type HomePageProps = {
  pathname?: string;
};

export function HomePage({ pathname }: HomePageProps = {}) {
  const apiBaseUrl = getOverlayApiBaseUrl();

  return (
    <main className="hud-shell hud-scanlines bg-transparent px-6 py-10 text-zinc-100">
      <section className="hud-panel hud-corners mx-auto max-w-5xl rounded-[1.6rem] p-8">
        <div className="flex flex-col gap-5 md:flex-row md:items-end md:justify-between">
          <div>
            <p className="hud-label">over-laid</p>
            <h1 className="mt-4 text-[clamp(2.6rem,5vw,4.4rem)] font-semibold tracking-tight text-white">Overlay routes</h1>
            <p className="mt-3 max-w-2xl text-zinc-300">
              Browser-source pages for OBS, starting with live alerts over SSE.
              Target streamer: <span className="font-medium text-white">patrick__eff</span>, with Twitch OAuth support for any streamer.
            </p>
          </div>

          <div className="flex flex-wrap gap-3 text-sm">
            <a className="hud-chip" href={`${apiBaseUrl}/api/auth/twitch/login`}>
              Twitch login
            </a>
            <a className="hud-chip" href={`${apiBaseUrl}/api/auth/twitch/status`}>
              Twitch status JSON
            </a>
          </div>
        </div>

        {pathname ? (
          <p className="mt-5 rounded-[1.2rem] border border-amber-400/30 bg-amber-400/10 px-4 py-3 text-sm text-amber-100">
            No overlay exists for <code>{pathname}</code>; pick a route below.
          </p>
        ) : null}

        <div className="mt-8 grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {routes.map(([href, title, description]) => (
            <a key={href} href={href} className="hud-panel hud-corners hud-home-card rounded-[1.25rem] p-4 transition duration-200 hover:-translate-y-0.5 hover:border-fuchsia-300/40">
              <p className="hud-kicker">{href}</p>
              <h2 className="mt-2 text-lg font-semibold text-white">{title}</h2>
              <p className="mt-2 text-sm leading-6 text-zinc-300">{description}</p>
            </a>
          ))}
        </div>
      </section>
    </main>
  );
}
