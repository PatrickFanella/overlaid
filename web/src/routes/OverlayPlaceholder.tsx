type OverlayPlaceholderPageProps = {
  pathname: string;
};

const copy: Record<string, { title: string; description: string }> = {
  '/overlay/chat': {
    title: 'Chat overlay',
    description: 'Placeholder route ready for Twitch chat rendering once the chat adapter lands.',
  },
  '/overlay/goals': {
    title: 'Goals overlay',
    description: 'Placeholder route for follower, sub, and channel-point goals.',
  },
  '/overlay/now-playing': {
    title: 'Now playing',
    description: 'Placeholder route for media and status widgets from self-hosted services.',
  },
  '/overlay/ticker': {
    title: 'Ticker overlay',
    description: 'Placeholder route for rotating event and status cards.',
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
    <main className="min-h-screen bg-transparent px-6 py-6 text-white">
      <section className="mx-auto max-w-xl rounded-3xl border border-white/10 bg-black/40 p-6 shadow-2xl shadow-black/40 backdrop-blur-md">
        <p className="text-xs uppercase tracking-[0.35em] text-fuchsia-200">{pathname}</p>
        <h1 className="mt-3 text-3xl font-semibold">{title}</h1>
        <p className="mt-3 text-sm text-zinc-200">{description}</p>
        <p className="mt-4 text-xs uppercase tracking-[0.25em] text-zinc-400">OBS browser-source ready</p>
      </section>
    </main>
  );
}
