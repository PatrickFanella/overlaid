const runbookBaseUrl =
  'https://git.subcult.tv/PatrickFanella/over-laid/src/branch/main/docs/runbooks';

const routeGuides = [
  {
    intent: 'monitor / recover',
    href: '/dashboard/live',
    label: 'Live ops cockpit',
    detail: 'Readiness, incidents, build identity, event history, and recovery runbooks.',
  },
  {
    intent: 'edit rules / profiles',
    href: '/dashboard/config',
    label: 'Config',
    detail: 'Operator secret, show profiles, Twitch aliases, and channel point reward rules.',
  },
  {
    intent: 'operate stream',
    href: '/dashboard/control',
    label: 'Stream control',
    detail: 'Quick actions, sequences, OBS controls, rehearsal, and BRB.',
  },
  {
    intent: 'wire OBS sources',
    href: '/dashboard/sources',
    label: 'Sources',
    detail: 'Overlay URLs, presets, and custom CSS.',
  },
  {
    intent: 'package / backup',
    href: `${runbookBaseUrl}/production-packaging.md`,
    label: 'Packaging runbook',
    detail: 'Version checks and api-data backup / restore commands.',
  },
] as const;

export function OpsRouteGuide() {
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">operator map</p>
      <h2 className="hud-section-title">Ops route guide</h2>
      <p className="hud-copy mt-1 text-xs text-zinc-300">
        Pick the route by the job, not by guessing which panel owns the state.
      </p>

      <div className="mt-4 grid gap-2 md:grid-cols-2 xl:grid-cols-3">
        {routeGuides.map((item) => (
          <a
            key={item.intent}
            className="hud-panel hud-corners hud-panel--flat hud-compact-card transition hover:border-[hsl(var(--signal))]"
            href={item.href}
          >
            <div className="hud-terminal-line">
              <span className="hud-terminal-key">intent</span>
              <span className="hud-terminal-value font-bold">{item.intent}</span>
            </div>
            <div className="hud-terminal-line">
              <span className="hud-terminal-key">route</span>
              <span className="hud-terminal-value">{item.label}</span>
            </div>
            <p className="mt-2 text-xs text-zinc-300">{item.detail}</p>
          </a>
        ))}
      </div>
    </section>
  );
}
