import { getOverlayApiBaseUrl } from '../lib/eventStream';

const routes = [
  ['/overlay/alerts', 'Live alert overlay', 'Fully functional SSE-driven alert browser source.'],
  ['/overlay/chat', 'Chat overlay', 'Placeholder browser source for chat rendering later.'],
  ['/overlay/goals', 'Goals overlay', 'Placeholder route for progress widgets.'],
  ['/overlay/now-playing', 'Now playing', 'Placeholder route for music/media status.'],
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
    <main className="min-h-screen bg-gradient-to-br from-zinc-950 via-zinc-900 to-slate-950 px-6 py-10 text-zinc-100">
      <section className="mx-auto max-w-4xl rounded-3xl border border-white/10 bg-white/5 p-8 shadow-2xl shadow-black/30 backdrop-blur">
        <p className="text-xs uppercase tracking-[0.35em] text-fuchsia-300">over-laid</p>
        <h1 className="mt-4 text-4xl font-semibold tracking-tight">Overlay routes</h1>
        <p className="mt-3 max-w-2xl text-zinc-300">
          Browser-source pages for OBS, starting with live alerts over SSE.
          Target streamer: <span className="font-medium text-white">patrick__eff</span>, with future OAuth support for any streamer.
        </p>

        <div className="mt-6 flex flex-wrap gap-3 text-sm">
          <a className="rounded-full border border-fuchsia-300/40 bg-fuchsia-300/10 px-4 py-2 text-fuchsia-100 transition hover:bg-fuchsia-300/20" href={`${apiBaseUrl}/api/auth/twitch/login`}>
            Twitch login
          </a>
          <a className="rounded-full border border-white/15 bg-white/5 px-4 py-2 text-zinc-100 transition hover:bg-white/10" href={`${apiBaseUrl}/api/auth/twitch/status`}>
            Twitch status JSON
          </a>
        </div>

        {pathname ? (
          <p className="mt-4 rounded-2xl border border-amber-400/30 bg-amber-400/10 px-4 py-3 text-sm text-amber-100">
            No overlay exists for <code>{pathname}</code>; pick a route below.
          </p>
        ) : null}

        <div className="mt-8 grid gap-4 md:grid-cols-2">
          {routes.map(([href, title, description]) => (
            <a
              key={href}
              href={href}
              className="rounded-2xl border border-white/10 bg-black/20 p-4 transition hover:border-fuchsia-300/40 hover:bg-white/10"
            >
              <p className="text-sm uppercase tracking-[0.25em] text-fuchsia-200">{href}</p>
              <h2 className="mt-2 text-lg font-semibold text-white">{title}</h2>
              <p className="mt-2 text-sm text-zinc-300">{description}</p>
            </a>
          ))}
        </div>
      </section>
    </main>
  );
}
