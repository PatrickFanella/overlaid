import type { CSSProperties } from 'react';

type OverlayPlaceholderPageProps = {
  pathname: string;
};

const copy: Record<string, { title: string; description: string }> = {
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
    case '/overlay/brb':
      return <BrbPreview />;
    default:
      return <GenericPreview />;
  }
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
