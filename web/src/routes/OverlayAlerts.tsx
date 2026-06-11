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

        if (event.type !== 'alert' || event.data?.retainedStateKey) {
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
    <main className="hud-shell hud-shell--alerts hud-scanlines">
      <div className="pointer-events-none mx-auto flex w-full max-w-4xl flex-col gap-4">
        <header className="hud-panel hud-corners hud-accent-fuchsia flex flex-wrap items-center justify-between gap-3 rounded-[1.4rem] px-5 py-4">
          <div>
            <p className="hud-label">Alert feed</p>
            <p className="mt-2 text-sm text-zinc-200">OBS browser source · live stack</p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <span className="hud-chip">
              <span className="hud-led" />
              {streamLabel}
            </span>
            <span className="hud-badge">{getOverlayApiBaseUrl()}</span>
          </div>
        </header>

        <section className="grid gap-3">
          {alerts.length === 0 ? (
            <div className="hud-panel hud-corners hud-accent-cyan rounded-[1.4rem] px-6 py-5">
              <div className="flex items-center gap-3">
                <span className="hud-led" />
                <p className="hud-label">Stand by</p>
              </div>
              <p className="mt-3 text-xl font-semibold tracking-tight">Waiting for alert events</p>
              <p className="mt-2 max-w-2xl text-sm text-zinc-300">Send a test alert to the SSE stream to preview the OBS browser source.</p>
            </div>
          ) : null}

          {alerts.map((alert) => (
            <article key={alert.id} className={`hud-panel hud-corners hud-alert-card hud-alert-card--${alert.severity} rounded-[1.4rem]`}>
              <div className="flex items-start gap-4 px-6 py-5">
                <div className="mt-1 flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl border border-white/10 bg-black/35">
                  <span className="hud-led" />
                </div>

                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="hud-badge">{alert.source}</span>
                    <span className="hud-badge">{severityLabel(alert.severity)}</span>
                    <span className="hud-badge">{formatClock(alert.createdAt)}</span>
                  </div>
                  <h2 className="mt-3 text-[clamp(1.5rem,2.8vw,2.45rem)] font-semibold leading-none tracking-tight text-white">{alert.title || 'Alert'}</h2>
                  <p className="mt-3 max-w-3xl text-base leading-relaxed text-zinc-100/90">{alert.message}</p>
                </div>
              </div>
            </article>
          ))}
        </section>
      </div>
    </main>
  );
}

function severityLabel(severity: AlertEntry['severity']) {
  switch (severity) {
    case 'success':
      return 'green room';
    case 'warning':
      return 'amber';
    case 'error':
      return 'critical';
    default:
      return 'info';
  }
}

function formatClock(value: string) {
  return new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}
