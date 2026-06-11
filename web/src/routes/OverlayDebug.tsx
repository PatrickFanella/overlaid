import { useEffect, useState } from 'react';
import { connectOverlayStream, getOverlayApiBaseUrl, type OverlayStreamStatus } from '../lib/eventStream';
import type { OverlayEvent } from '../lib/events';
import { formatOverlayStreamLabel } from '../lib/events';

export function OverlayDebugPage() {
  const [events, setEvents] = useState<OverlayEvent[]>([]);
  const [status, setStatus] = useState<OverlayStreamStatus>('connecting');

  useEffect(() => {
    const stream = connectOverlayStream(getOverlayApiBaseUrl(), {
      onEvent(event) {
        setEvents((current) => [event, ...current].slice(0, 8));
      },
      onStatusChange(nextStatus) {
        setStatus(nextStatus);
      },
    });

    return () => {
      stream.close();
    };
  }, []);

  return (
    <main className="hud-shell hud-shell--compact hud-shell--debug hud-scanlines text-white">
      <section className="hud-panel hud-corners hud-accent-cyan w-full max-w-4xl rounded-[1.4rem] p-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <p className="hud-label">/overlay/debug</p>
            <h1 className="mt-3 text-[clamp(2rem,4vw,3.4rem)] font-semibold tracking-tight">Event inspector</h1>
            <p className="mt-2 text-sm text-zinc-300">Live SSE feed from {getOverlayApiBaseUrl()}.</p>
          </div>
          <span className="hud-chip">
            <span className="hud-led" />
            {formatOverlayStreamLabel(status)}
          </span>
        </div>

        <div className="mt-5 grid gap-3">
          {events.length === 0 ? (
            <p className="hud-panel hud-corners rounded-[1.25rem] border border-dashed border-white/10 px-4 py-5 text-sm text-zinc-400">
              Waiting for events…
            </p>
          ) : null}

          {events.map((event) => (
            <pre key={event.id} className="hud-panel hud-corners hud-event-json rounded-[1.25rem] px-4 py-4">
              {JSON.stringify(event, null, 2)}
            </pre>
          ))}
        </div>
      </section>
    </main>
  );
}
