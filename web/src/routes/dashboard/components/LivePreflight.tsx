import type { OBSStatus, TwitchStatus } from '../../../lib/api';
import type { DashboardConfig } from '../../../lib/dashboardConfig';

type LivePreflightProps = {
  twitch: TwitchStatus | null;
  obs: OBSStatus | null;
  config: DashboardConfig;
  operatorSecret: string;
};

export function LivePreflight({ twitch, obs, config, operatorSecret }: LivePreflightProps) {
  const checks = [
    { label: 'Twitch authenticated', ok: Boolean(twitch?.authenticated), fix: 'Open Twitch login from the header.' },
    { label: 'EventSub connected', ok: Boolean(twitch?.eventSub?.connected), fix: 'Wait for EventSub or re-login if scopes changed.' },
    { label: 'OBS reachable', ok: Boolean(obs?.reachable), fix: 'Check OBS WebSocket, host networking, and password.' },
    { label: 'Operator secret loaded', ok: Boolean(operatorSecret), fix: 'Open Config and enter the operator secret.' },
    { label: 'Setup checklist complete', ok: config.setupChecklist.every((item) => item.completed), fix: 'Open Setup and complete remaining items.' },
  ];

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Live preflight</p>
      <div className="mt-3 grid gap-2 md:grid-cols-2 xl:grid-cols-5">
        {checks.map((check) => (
          <article key={check.label} className={`hud-panel hud-corners hud-panel--flat hud-compact-card ${check.ok ? 'hud-accent-emerald' : 'hud-accent-rose'}`}>
            <p className="hud-kicker">{check.ok ? 'ready' : 'fix'}</p>
            <p className="mt-1 text-sm font-bold text-white">{check.label}</p>
            {!check.ok ? <p className="mt-1 text-xs text-zinc-300">{check.fix}</p> : null}
          </article>
        ))}
      </div>
    </section>
  );
}
