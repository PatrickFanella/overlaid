import type { EventHistoryItem } from '../../../lib/api';

type EventHistoryPanelProps = {
  events: EventHistoryItem[];
  loading?: boolean;
  error?: string;
};

function formatEventTypeLabel(type: string) {
  return type.replaceAll('_', ' ');
}

export function EventHistoryPanel({ events, loading = false, error = '' }: EventHistoryPanelProps) {
  const visibleEvents = events.slice(0, 12);

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="hud-label">persisted stream data</p>
          <h2 className="hud-section-title">Event history</h2>
          <p className="hud-copy mt-1 text-xs text-zinc-300">Recent sanitized events from <code>/api/events/history</code>.</p>
        </div>
        <span className="hud-chip hud-chip--square">{loading ? 'loading' : `${events.length} events`}</span>
      </div>

      {error ? <p className="mt-3 text-sm text-rose-300">{error}</p> : null}
      {!loading && visibleEvents.length === 0 ? <p className="mt-3 text-sm text-zinc-400">No persisted events yet.</p> : null}

      <div className="mt-4 grid gap-2">
        {visibleEvents.map((event) => (
          <article key={event.id} className="hud-panel hud-corners hud-panel--flat hud-compact-card">
            <div className="hud-terminal-line">
              <span className="hud-terminal-key">type</span>
              <span className="hud-terminal-value font-bold">{formatEventTypeLabel(event.type)}</span>
            </div>
            <div className="hud-terminal-line">
              <span className="hud-terminal-key">source</span>
              <span className="hud-terminal-value">{event.source}</span>
            </div>
            {event.title ? (
              <div className="hud-terminal-line">
                <span className="hud-terminal-key">title</span>
                <span className="hud-terminal-value">{event.title}</span>
              </div>
            ) : null}
            {event.message ? (
              <div className="hud-terminal-line">
                <span className="hud-terminal-key">message</span>
                <span className="hud-terminal-value">{event.message}</span>
              </div>
            ) : null}
            <div className="hud-terminal-line">
              <span className="hud-terminal-key">time</span>
              <span className="hud-terminal-value">{event.createdAt}</span>
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}
