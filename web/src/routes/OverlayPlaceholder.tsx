import type { CSSProperties } from 'react';

type OverlayPlaceholderPageProps = {
  pathname: string;
};

const copy: Record<string, { title: string; description: string }> = {
  '/overlay/now-playing': {
    title: 'Now playing',
    description: 'Placeholder route for media and status widgets from self-hosted services.',
  },
  '/overlay/brb': {
    title: 'BRB overlay',
    description: 'Placeholder route for intermission and scene-break screens.',
  },
};

export function OverlayPlaceholderPage({ pathname }: OverlayPlaceholderPageProps) {
  const { title, description } = copy[pathname] ?? {
    title: 'Overlay placeholder',
    description: 'Route reserved for future overlay work.',
  };

  return (
    <main className="hud-shell hud-shell--compact hud-scanlines text-white">
      <section className="hud-panel hud-corners mx-auto w-full max-w-4xl rounded-[1.4rem] p-6">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <p className="hud-label">{pathname}</p>
            <h1 className="mt-3 text-[clamp(2rem,4vw,3.5rem)] font-semibold tracking-tight">{title}</h1>
            <p className="mt-3 max-w-2xl text-sm leading-6 text-zinc-200">{description}</p>
          </div>
          <span className="hud-chip">
            <span className="hud-led" />
            OBS browser-source ready
          </span>
        </div>

        <div className="mt-6">{renderOverlayPreview(pathname)}</div>

        <p className="mt-6 text-xs uppercase tracking-[0.28em] text-zinc-400">OBS browser-source ready</p>
      </section>
    </main>
  );
}

function renderOverlayPreview(pathname: string) {
  switch (pathname) {
    case '/overlay/now-playing':
      return <NowPlayingPreview />;
    case '/overlay/brb':
      return <BrbPreview />;
    default:
      return <GenericPreview />;
  }
}

function NowPlayingPreview() {
  return (
    <section className="hud-panel hud-corners hud-accent-cyan grid gap-4 rounded-[1.4rem] p-5 lg:grid-cols-[180px_1fr] lg:items-center">
      <div className="flex items-center justify-center rounded-[1.4rem] border border-white/10 bg-[radial-gradient(circle_at_top_left,rgba(103,232,249,0.35),transparent_42%),linear-gradient(160deg,rgba(255,255,255,0.08),rgba(0,0,0,0.2))] p-6 shadow-inner shadow-black/40">
        <div className="flex h-32 w-32 items-center justify-center rounded-[1.1rem] border border-white/15 bg-black/45 text-4xl font-black tracking-[0.18em] text-cyan-100">
          NP
        </div>
      </div>

      <div>
        <p className="hud-label">Now playing</p>
        <h2 className="mt-3 text-[clamp(1.7rem,3.2vw,3rem)] font-semibold leading-none tracking-tight text-white">Stream intermission mix</h2>
        <p className="mt-3 max-w-2xl text-sm leading-6 text-zinc-200">Track metadata, playback state, and service status can live here without changing the browser-source layout.</p>
        <div className="mt-5">
          <div className="hud-progress" style={{ '--hud-progress': '54%' } as CSSProperties}>
            <div className="hud-progress__fill" />
          </div>
          <div className="mt-3 flex items-center justify-between text-xs uppercase tracking-[0.24em] text-zinc-400">
            <span>02:14 elapsed</span>
            <span>04:01 total</span>
          </div>
        </div>
      </div>
    </section>
  );
}

function BrbPreview() {
  return (
    <section className="grid items-center gap-4 lg:grid-cols-[1fr_0.8fr]">
      <div className="hud-panel hud-corners hud-accent-rose rounded-[1.4rem] p-6 text-center lg:text-left">
        <p className="hud-label">Intermission</p>
        <h2 className="mt-4 text-[clamp(3.4rem,9vw,7rem)] font-black leading-none tracking-[-0.08em] text-white">BRB</h2>
        <p className="mt-4 max-w-xl text-sm leading-6 text-zinc-200">Scene break, quick reset, or a short reset screen with the same broadcast visual language.</p>
      </div>

      <div className="grid gap-3">
        {['Be right back', 'Music muted', 'Chat paused'].map((item, index) => (
          <div key={item} className="hud-panel hud-corners rounded-[1.25rem] p-4" style={{ '--hud-accent': ['var(--hud-rose)', 'var(--hud-amber)', 'var(--hud-cyan)'][index] } as CSSProperties}>
            <p className="hud-kicker">{item}</p>
          </div>
        ))}
      </div>
    </section>
  );
}

function GenericPreview() {
  return (
    <section className="hud-panel hud-corners hud-accent-cyan rounded-[1.4rem] p-5">
      <p className="hud-kicker">Overlay frame</p>
      <p className="mt-3 text-xl font-semibold text-white">Route reserved for future overlay work.</p>
    </section>
  );
}
