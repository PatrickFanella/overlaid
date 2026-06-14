import { getDashboardRoute } from '../../../lib/dashboardRoutes';

const runbookBaseUrl =
  'https://git.subcult.tv/PatrickFanella/over-laid/src/branch/main/docs/runbooks';

const liveRoute = getDashboardRoute('/dashboard/live');
const configRoute = getDashboardRoute('/dashboard/config');
const controlRoute = getDashboardRoute('/dashboard/control');
const sourcesRoute = getDashboardRoute('/dashboard/sources');

const routeGuides = [
  {
    intent: 'monitor / recover',
    href: liveRoute?.path ?? '/dashboard/live',
    label: liveRoute?.title ?? 'Live ops cockpit',
    detail: liveRoute?.description ?? 'Readiness, incidents, build identity, event history, and recovery runbooks.',
  },
  {
    intent: 'edit rules / profiles',
    href: configRoute?.path ?? '/dashboard/config',
    label: configRoute?.navLabel ?? 'Config',
    detail: configRoute?.description ?? 'Operator secret, show profiles, Twitch aliases, and channel point reward rules.',
  },
  {
    intent: 'operate stream',
    href: controlRoute?.path ?? '/dashboard/control',
    label: controlRoute?.title ?? 'Stream control',
    detail: controlRoute?.description ?? 'Quick actions, sequences, OBS controls, rehearsal, and BRB.',
  },
  {
    intent: 'wire OBS sources',
    href: sourcesRoute?.path ?? '/dashboard/sources',
    label: sourcesRoute?.navLabel ?? 'Sources',
    detail: sourcesRoute?.description ?? 'Overlay URLs, presets, and custom CSS.',
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
