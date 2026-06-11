import { useEffect, useMemo, useState } from 'react';
import { connectOverlayStream, getOverlayApiBaseUrl, type OverlayStreamStatus } from '../lib/eventStream';
import { formatOverlayStreamLabel, type OverlayEvent } from '../lib/events';

type AlertEntry = OverlayEvent;

const maxAlerts = 3;

export function OverlayAlertsPage() {
  const [alerts, setAlerts] = useState<AlertEntry[]>([]);
  const [status, setStatus] = useState<OverlayStreamStatus>('connecting');

  useEffect(() => {
    const baseUrl = getOverlayApiBaseUrl();
    const stream = connectOverlayStream(baseUrl, {
      onEvent(event) {
        if (event.type === 'clear') {
          setAlerts([]);
          return;
        }

        if (event.type !== 'alert') {
          return;
        }

        setAlerts((current) => [event, ...current].slice(0, maxAlerts));
      },
      onStatusChange(nextStatus) {
        setStatus(nextStatus);
      },
    });

    return () => {
      stream.close();
    };
  }, []);

  const streamLabel = useMemo(() => formatOverlayStreamLabel(status), [status]);

  return (
    <main className="min-h-screen bg-transparent px-8 py-8 text-white">
      <div className="pointer-events-none mx-auto flex max-w-3xl flex-col gap-4">
        <header className="self-start rounded-full border border-white/15 bg-black/45 px-4 py-2 text-xs uppercase tracking-[0.3em] text-zinc-200 backdrop-blur-md">
          Alerts · {streamLabel} · {getOverlayApiBaseUrl()}
        </header>

        <section className="flex flex-col gap-3">
          {alerts.length === 0 ? (
            <div className="rounded-3xl border border-white/10 bg-black/35 px-6 py-5 shadow-2xl shadow-black/40 backdrop-blur-md">
              <p className="text-lg font-semibold">Waiting for alert events</p>
              <p className="mt-1 text-sm text-zinc-300">Send a test alert to the SSE stream to preview the OBS browser source.</p>
            </div>
          ) : null}

          {alerts.map((alert) => (
            <article
              key={alert.id}
              className="overflow-hidden rounded-3xl border border-white/12 bg-gradient-to-r from-fuchsia-500/20 via-black/50 to-cyan-500/15 shadow-2xl shadow-black/45 backdrop-blur-md"
            >
              <div className="flex items-start gap-4 px-6 py-5">
                <div className="mt-1 h-3 w-3 rounded-full bg-fuchsia-300 shadow-[0_0_18px_rgba(232,121,249,0.85)]" />
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2 text-xs uppercase tracking-[0.3em] text-fuchsia-100/90">
                    <span>{alert.source}</span>
                    <span>·</span>
                    <span>{alert.severity}</span>
                  </div>
                  <h2 className="mt-2 text-3xl font-semibold leading-tight text-white">{alert.title || 'Alert'}</h2>
                  <p className="mt-2 text-base text-zinc-100/90">{alert.message}</p>
                </div>
              </div>
            </article>
          ))}
        </section>
      </div>
    </main>
  );
}
