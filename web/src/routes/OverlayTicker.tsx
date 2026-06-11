import { useEffect, useMemo, useState } from 'react';
import { connectOverlayStream, getOverlayApiBaseUrl, type OverlayStreamStatus } from '../lib/eventStream';
import { formatOverlayStreamLabel, type OverlayEvent, type OverlaySeverity } from '../lib/events';

type TickerItem = {
  id: string;
  label: string;
  message: string;
  severity: OverlaySeverity;
  source: string;
};

const maxTickerItems = 8;

export function OverlayTickerPage() {
  const [items, setItems] = useState<TickerItem[]>([]);
  const [status, setStatus] = useState<OverlayStreamStatus>('connecting');

  useEffect(() => {
    const stream = connectOverlayStream(getOverlayApiBaseUrl(), {
      onEvent(event) {
        if (event.type === 'clear') {
          setItems([]);
          return;
        }

        const item = tickerItemFromEvent(event);
        if (!item) {
          return;
        }

        setItems((current) => [item, ...current.filter((existing) => existing.id !== item.id)].slice(0, maxTickerItems));
      },
      onStatusChange(nextStatus) {
        setStatus(nextStatus);
      },
    });

    return () => stream.close();
  }, []);

  const streamLabel = useMemo(() => formatOverlayStreamLabel(status), [status]);

  return (
    <main className="hud-shell hud-shell--ticker hud-scanlines text-white">
      <section className="hud-panel hud-corners hud-accent-fuchsia pointer-events-none w-full rounded-[1.4rem] p-4">
        <div className="flex flex-wrap items-center gap-3">
          <span className="hud-chip hud-accent-rose">
            <span className="hud-led" />
            {streamLabel}
          </span>
          <span className="hud-badge">Ticker rail</span>
          <span className="hud-badge">{items.length} items</span>
        </div>

        <div role="log" aria-live="polite" aria-relevant="additions text" className="mt-4 overflow-hidden rounded-[1.1rem] border border-white/10 bg-black/35 px-4 py-3">
          <div className="hud-ticker-track">
            {(items.length ? [...items, ...items] : [{ id: 'standby', label: 'Ticker rail', message: 'Waiting for ticker events', severity: 'info' as const, source: 'over-laid' }]).map((item, index) => (
              <article key={`${item.id}-${index}`} className={`hud-ticker-item hud-alert-card--${item.severity}`}>
                <span className="hud-kicker text-white">{item.label}</span>
                <span className="text-sm font-semibold text-zinc-100">{item.message}</span>
                <span className="hud-badge">{item.source}</span>
              </article>
            ))}
          </div>
        </div>
      </section>
    </main>
  );
}

function tickerItemFromEvent(event: OverlayEvent): TickerItem | null {
  const data = isRecord(event.data) ? event.data : undefined;
  const ticker = isRecord(data?.ticker) ? data.ticker : undefined;
  if (data?.retainedStateKey && data.retainedStateKey !== 'ticker') {
    return null;
  }
  const label = asString(ticker?.label) || asString(data?.tickerLabel) || event.title || event.type.replaceAll('_', ' ');
  const message = asString(ticker?.message) || asString(data?.tickerMessage) || event.message || event.title;

  if (!message) {
    return null;
  }

  return {
    id: asString(ticker?.id) || event.id,
    label,
    message,
    severity: event.severity,
    source: event.source,
  };
}

function asString(value: unknown) {
  return typeof value === 'string' ? value.trim() : '';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
