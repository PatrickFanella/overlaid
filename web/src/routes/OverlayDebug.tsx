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
    <main className="min-h-screen bg-transparent px-6 py-6 text-white">
      <section className="mx-auto max-w-3xl rounded-3xl border border-white/10 bg-black/45 p-6 shadow-2xl shadow-black/40 backdrop-blur-md">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <p className="text-xs uppercase tracking-[0.35em] text-fuchsia-200">/overlay/debug</p>
            <h1 className="mt-2 text-3xl font-semibold">Event inspector</h1>
          </div>
          <p className="rounded-full border border-white/15 bg-white/5 px-3 py-1 text-xs uppercase tracking-[0.25em] text-zinc-200">
            {formatOverlayStreamLabel(status)}
          </p>
        </div>

        <p className="mt-4 text-sm text-zinc-300">Live SSE feed from {getOverlayApiBaseUrl()}.</p>

        <div className="mt-5 space-y-3">
          {events.length === 0 ? (
            <p className="rounded-2xl border border-dashed border-white/10 px-4 py-5 text-sm text-zinc-400">
              Waiting for events…
            </p>
          ) : null}

          {events.map((event) => (
            <pre key={event.id} className="overflow-auto rounded-2xl border border-white/10 bg-black/50 p-4 text-xs leading-6 text-zinc-100">
              {JSON.stringify(event, null, 2)}
            </pre>
          ))}
        </div>
      </section>
    </main>
  );
}
