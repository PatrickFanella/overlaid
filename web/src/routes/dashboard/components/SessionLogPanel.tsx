import type { SessionLogEntry } from '../../../lib/sessionLog';

type SessionLogPanelProps = {
  entries: SessionLogEntry[];
  onClear?: () => void;
};

export function SessionLogPanel({ entries, onClear }: SessionLogPanelProps) {
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

      <div className="mt-3 grid gap-2" role="log" aria-live="polite" aria-relevant="additions text">
        {entries.length === 0 ? <p className="text-sm text-zinc-400">No actions recorded this session.</p> : null}

        {entries.map((entry) => (
          <article
            key={entry.id}
            className={`hud-panel hud-corners hud-panel--flat hud-compact-card ${
              entry.status === 'success' ? 'hud-accent-emerald' : entry.status === 'error' ? 'hud-accent-rose' : 'hud-accent-cyan'
            }`}
          >
            <div className="hud-terminal-line">
              <span className="hud-terminal-key">{entry.status}</span>
              <span className="hud-terminal-value font-bold">{entry.label}</span>
            </div>
            <div className="hud-terminal-line">
              <span className="hud-terminal-key">time</span>
              <span className="hud-terminal-value">{entry.createdAt}</span>
            </div>
            <p className="mt-2 text-xs text-zinc-300">{entry.message}</p>
          </article>
        ))}
      </div>
    </section>
  );
}
