import type { EventHistoryItem } from '../../../lib/api';
import { Empty, Notice, Panel, Tag, type Tone } from '../ui';

type EventHistoryPanelProps = {
  events: EventHistoryItem[];
  loading?: boolean;
  error?: string;
  limit?: number;
  title?: string;
};

function formatEventTypeLabel(type: string) {
  return type.replaceAll('_', ' ');
}

function toneForSeverity(severity: string | undefined): Tone {
  if (severity === 'error') return 'bad';
  if (severity === 'warning') return 'warn';
  if (severity === 'success') return 'ok';
  return 'neutral';
}

export function EventHistoryPanel({ events, loading = false, error = '', limit = 12, title = 'Event history' }: EventHistoryPanelProps) {
  const visibleEvents = events.slice(0, limit);

  return (
    <Panel
      kicker="Persisted events"
      title={title}
      description={
        <>
          Sanitized events from <code>/api/events/history</code>, newest first.
        </>
      }
      actions={<Tag>{loading ? 'loading' : `${events.length} events`}</Tag>}
      flush
    >
      {error ? (
        <div className="p-4">
          <Notice tone="bad">{error}</Notice>
        </div>
      ) : null}

      <ol className="desk-rows">
        {!loading && visibleEvents.length === 0 ? (
          <li className="desk-row">
            <Empty>No persisted events yet.</Empty>
          </li>
        ) : null}

        {visibleEvents.map((event) => (
          <li key={event.id} className={`desk-row desk-row--${toneForSeverity(event.severity)}`}>
            <div className="desk-row__line">
              <p className="desk-row__title">{event.title || event.message || formatEventTypeLabel(event.type)}</p>
              <time className="desk-row__meta mono" dateTime={event.createdAt}>
                {event.createdAt}
              </time>
            </div>
            {event.title && event.message ? <p className="desk-row__text">{event.message}</p> : null}
            <p className="desk-row__meta mono">
              {formatEventTypeLabel(event.type)} · {event.source}
              {typeof event.data?.sessionId === 'string' ? ` · session ${event.data.sessionId.slice(0, 8)}` : ''}
            </p>
          </li>
        ))}
      </ol>
    </Panel>
  );
}
