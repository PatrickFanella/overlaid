import { useEffect, useMemo, useState } from 'react';
import { loadDashboardConfig } from '../lib/dashboardConfig';
import { connectOverlayStream, getOverlayApiBaseUrl, type OverlayStreamStatus } from '../lib/eventStream';
import { formatOverlayStreamLabel, type OverlayEvent } from '../lib/events';

type AlertEntry = OverlayEvent;

export function OverlayAlertsPage() {
  const display = loadDashboardConfig().display;
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

        setAlerts((current) => {
          if (isDuplicateAlert(current, event, display.alertDedupeSeconds)) {
            return current;
          }
          return [event, ...current].slice(0, display.alertMaxVisible);
        });
      },
      onStatusChange(nextStatus) {
        setStatus(nextStatus);
      },
    });

    return () => {
      stream.close();
    };
  }, [display.alertDedupeSeconds, display.alertMaxVisible]);

  const streamLabel = useMemo(() => formatOverlayStreamLabel(status), [status]);

  return (
    <main className="hud-shell hud-shell--alerts hud-scanlines">
      <div className="pointer-events-none mx-auto flex w-full max-w-4xl flex-col gap-4">
        <header className="hud-panel hud-corners hud-accent-fuchsia flex flex-wrap items-center justify-between gap-3 px-4 py-3">
          <div>
            <p className="hud-label">Alert feed</p>
            <p className="mt-1 text-xs text-zinc-300">/feed :: alerts</p>
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
            <div className="hud-panel hud-corners hud-accent-cyan px-5 py-4">
              <div className="flex items-center gap-3">
                <span className="hud-led" />
                <p className="hud-label">Stand by</p>
              </div>
              <p className="mt-3 text-xl font-bold tracking-tight">Waiting for alert events</p>
              <p className="mt-2 max-w-2xl text-sm text-zinc-300">terminal ready :: send test alert</p>
            </div>
          ) : null}

          {alerts.map((alert) => (
            <article key={alert.id} className={`hud-panel hud-corners hud-alert-card hud-alert-card--${alert.severity}`}>
              <div className="flex items-start gap-4 px-5 py-4">
                <div className="mt-1 flex h-9 w-9 shrink-0 items-center justify-center border-2 border-white/10 bg-black/60">
                  <span className="hud-led" />
                </div>

                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="hud-badge">{alert.source}</span>
                    <span className="hud-badge">{severityLabel(alert.severity)}</span>
                    <span className="hud-badge">{formatClock(alert.createdAt)}</span>
                  </div>
                  <h2 className="mt-3 text-[clamp(1.35rem,2.4vw,2.1rem)] font-black leading-none tracking-[-0.04em] text-white">{alert.title || 'Alert'}</h2>
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

function isDuplicateAlert(current: AlertEntry[], next: AlertEntry, dedupeSeconds: number) {
  if (dedupeSeconds <= 0) {
    return false;
  }

  const cutoff = Date.now() - dedupeSeconds * 1000;
  return current.some((alert) => alert.title === next.title && alert.message === next.message && Date.parse(alert.createdAt) >= cutoff);
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
