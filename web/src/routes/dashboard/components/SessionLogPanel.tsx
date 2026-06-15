import type { SessionLogEntry } from '../../../lib/sessionLog';

type SessionLogPanelProps = {
  entries: SessionLogEntry[];
  onClear?: () => void;
};

export function SessionLogPanel({ entries, onClear }: SessionLogPanelProps) {
  const latest = entries[0];
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="hud-label">Session log</p>
          <h2 className="hud-section-title">Recent operator actions</h2>
        </div>
        {onClear ? (
          <button className="hud-chip hud-chip--square" type="button" onClick={onClear}>
            clear log
          </button>
        ) : null}
      </div>

      {latest ? (
        <article className={`hud-panel hud-corners hud-panel--flat hud-compact-card mt-3 ${accentForStatus(latest.status)}`} role="status" aria-live="polite">
          <p className="hud-kicker">Latest action · {latest.status}</p>
          <p className="mt-1 text-sm font-bold text-white">{latest.label}</p>
          <p className="mt-1 text-xs text-zinc-300">{latest.message}</p>
        </article>
      ) : null}

      <div className="mt-3 grid gap-2" role="log" aria-live="polite" aria-relevant="additions text">
        {entries.length === 0 ? <p className="text-sm text-zinc-400">No actions recorded this session.</p> : null}

        {entries.map((entry) => (
          <article
            key={entry.id}
            className={`hud-panel hud-corners hud-panel--flat hud-compact-card ${accentForStatus(entry.status)}`}
          >
            <div className="hud-terminal-line">
              <span className="hud-terminal-key">{entry.status}</span>
              <span className="hud-terminal-value font-bold">{entry.label}</span>
            </div>
            <div className="hud-terminal-line">
              <span className="hud-terminal-key">time</span>
              <span className="hud-terminal-value">{new Date(entry.createdAt).toLocaleTimeString()}</span>
            </div>
            <p className="mt-2 text-xs text-zinc-300">{entry.message}</p>
          </article>
        ))}
      </div>
    </section>
  );
}

function accentForStatus(status: SessionLogEntry['status']) {
  if (status === 'success') return 'hud-accent-emerald';
  if (status === 'error') return 'hud-accent-rose';
  return 'hud-accent-cyan';
}
