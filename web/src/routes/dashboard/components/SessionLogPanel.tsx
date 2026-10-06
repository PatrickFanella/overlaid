import type { SessionLogEntry } from '../../../lib/sessionLog';
import { Empty, Panel, type Tone } from '../ui';

type SessionLogPanelProps = {
  entries: SessionLogEntry[];
  onClear?: () => void;
  limit?: number;
};

export function SessionLogPanel({ entries, onClear, limit }: SessionLogPanelProps) {
  const latest = entries[0];
  const visible = limit ? entries.slice(0, limit) : entries;

  return (
    <Panel
      kicker="Session log"
      title="Recent operator actions"
      flush
      actions={
        onClear ? (
          <button className="desk-btn desk-btn--small desk-btn--ghost" type="button" onClick={onClear}>
            Clear log
          </button>
        ) : null
      }
    >
      {latest ? (
        <p className="sr-only" role="status" aria-live="polite">
          Latest action · {latest.status}: {latest.label}
        </p>
      ) : null}

      <ol className="desk-rows" role="log" aria-live="polite" aria-relevant="additions text">
        {entries.length === 0 ? (
          <li className="desk-row">
            <Empty>No actions recorded this session.</Empty>
          </li>
        ) : null}

        {visible.map((entry, index) => (
          <li key={entry.id} className={`desk-row desk-row--${toneForStatus(entry.status)}`}>
            <div className="desk-row__line">
              <p className="desk-row__title">
                {index === 0 ? <span className="desk-panel__kicker">Latest action · </span> : null}
                {entry.label}
              </p>
              <span className="desk-row__meta mono">
                {entry.status} · {new Date(entry.createdAt).toLocaleTimeString()}
              </span>
            </div>
            <p className="desk-row__text">{entry.message}</p>
          </li>
        ))}
      </ol>
    </Panel>
  );
}

function toneForStatus(status: SessionLogEntry['status']): Tone {
  if (status === 'success') return 'ok';
  if (status === 'error') return 'bad';
  return 'neutral';
}
