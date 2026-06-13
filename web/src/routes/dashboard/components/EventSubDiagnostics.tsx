import type { TwitchStatus } from '../../../lib/api';

export function EventSubDiagnostics({ status }: { status: TwitchStatus | null }) {
  const eventSub = status?.eventSub;

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Twitch EventSub</p>
      <div className="mt-2 grid gap-1">
        <div className="hud-terminal-line"><span className="hud-terminal-key">link</span><span className="hud-terminal-value">{eventSub?.connected ? 'connected' : 'not connected'}</span></div>
        <div className="hud-terminal-line"><span className="hud-terminal-key">subs</span><span className="hud-terminal-value">{eventSub ? `${eventSub.subscriptionsMade}/${eventSub.subscriptionsPlan}` : 'n/a'}</span></div>
        <div className="hud-terminal-line"><span className="hud-terminal-key">last event</span><span className="hud-terminal-value">{eventSub?.lastEventAt || 'waiting'}</span></div>
      </div>
      {eventSub?.lastError ? <p className="mt-2 border border-red-400/40 bg-black px-2 py-1 text-xs text-rose-200">error :: {eventSub.lastError}</p> : null}
    </section>
  );
}
