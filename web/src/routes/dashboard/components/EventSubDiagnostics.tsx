import type { TwitchStatus } from '../../../lib/api';

export function EventSubDiagnostics({ status }: { status: TwitchStatus | null }) {
  const eventSub = status?.eventSub;

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Twitch EventSub</p>
      <h2 className="hud-section-title">{eventSub?.connected ? 'Connected' : 'Not connected'}</h2>
      <p className="mt-2 text-xs text-zinc-300">
        {eventSub ? `${eventSub.subscriptionsMade}/${eventSub.subscriptionsPlan} subscriptions active` : 'Diagnostics unavailable'}
      </p>
      {eventSub?.lastError ? <p className="mt-2 text-xs text-rose-200">{eventSub.lastError}</p> : null}
    </section>
  );
}
