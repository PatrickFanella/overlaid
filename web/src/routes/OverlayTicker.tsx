import { useState, type CSSProperties } from 'react';
import { loadDashboardConfig } from '../lib/dashboardConfig';
import type { OverlayEvent, OverlaySeverity } from '../lib/events';
import { useOverlayStream } from '../lib/useOverlayStream';
import { Mark, OverlayStage } from '../overlay/Brand';
import { asString, isRecord } from '../overlay/data';

type TickerItem = {
  id: string;
  label: string;
  message: string;
  severity: OverlaySeverity;
};

// Seconds of scroll per item, so a long rail does not race.
const SECONDS_PER_ITEM = 9;

export function OverlayTickerPage() {
  const [display] = useState(() => loadDashboardConfig().display);
  const [items, setItems] = useState<TickerItem[]>([]);

  const status = useOverlayStream((event) => {
    if (event.type === 'clear') {
      setItems([]);
      return;
    }

    const item = tickerItemFromEvent(event);
    if (!item) {
      return;
    }

    setItems((current) => [item, ...current.filter((existing) => existing.id !== item.id)].slice(0, display.tickerMaxItems));
  });

  const scrolling = items.length > 1;
  // The track holds two copies so the -50% marquee loops without a seam.
  const track = scrolling ? [...items, ...items] : items;

  return (
    <OverlayStage name="ticker" status={status} label="Ticker rail">
      <section className="sc-ticker">
        <div className="sc-ticker__brand">
          <Mark size={48} />
          <span className="sc-display">On the wire</span>
        </div>

        <div role="log" aria-live="polite" aria-relevant="additions text" className="sc-ticker__window">
          {items.length === 0 ? (
            <div className="sc-ticker__track sc-ticker__track--still sc-standby-row">
              <article className="sc-ticker__item">
                <span className="sc-label sc-ticker__tag">standby</span>
                <span className="sc-ticker__message">Waiting for ticker events</span>
              </article>
            </div>
          ) : (
            <div
              className={`sc-ticker__track ${scrolling ? '' : 'sc-ticker__track--still'}`}
              style={{ '--ticker-duration': `${items.length * SECONDS_PER_ITEM}s` } as CSSProperties}
            >
              {track.map((item, index) => (
                <article key={`${item.id}-${index}`} className={`sc-ticker__item sc-ticker__item--${item.severity}`} aria-hidden={index >= items.length || undefined}>
                  <span className="sc-label sc-ticker__tag">{item.label}</span>
                  <span className="sc-ticker__message">{item.message}</span>
                </article>
              ))}
            </div>
          )}
        </div>
      </section>
    </OverlayStage>
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
  };
}
