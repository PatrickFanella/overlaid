import type { ReadinessReport } from '../../../lib/api';

type ReadinessPanelProps = {
  report: ReadinessReport | null;
  loading: boolean;
  error: string;
};

function formatCheckName(name: string) {
  return name.replaceAll('_', ' ');
}

export function ReadinessPanel({ report, loading, error }: ReadinessPanelProps) {
  const degraded = report?.checks.filter((check) => check.status !== 'ok') ?? [];

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="hud-label">Readiness</p>
          <h2 className="hud-section-title">Backend readiness</h2>
          <p className="hud-copy mt-1 text-xs text-zinc-300">
            Mirrors <code>/readyz</code>; degraded means operator attention, not process death.
          </p>
        </div>
        <span className={`hud-chip hud-chip--square ${report?.status === 'ok' ? 'hud-accent-emerald' : 'hud-accent-amber'}`}>
          {loading ? 'checking' : report?.status ?? 'unknown'}
        </span>
      </div>

      {error ? <p className="mt-3 text-sm text-rose-300">{error}</p> : null}

      {degraded.length > 0 ? (
        <div className="mt-3 grid gap-2">
          <p className="hud-label">Operator incidents</p>
          {degraded.map((check) => (
            <article key={check.name} className="hud-panel hud-corners hud-panel--flat hud-compact-card hud-accent-amber">
              <div className="hud-terminal-line">
                <span className="hud-terminal-key">incident</span>
                <span className="hud-terminal-value font-bold">{formatCheckName(check.name)}</span>
              </div>
              <p className="mt-2 text-xs text-zinc-300">{check.hint || 'Check production recovery runbook.'}</p>
            </article>
          ))}
        </div>
      ) : null}

      <div className="mt-3 grid gap-2 md:grid-cols-2">
        {(report?.checks ?? []).map((check) => (
          <div key={check.name} className={`hud-terminal-line ${check.status === 'ok' ? 'text-emerald-200' : 'text-amber-200'}`}>
            <span className="hud-terminal-key">{formatCheckName(check.name)}</span>
            <span className="hud-terminal-value">{check.status}</span>
          </div>
        ))}
        {!loading && !report ? <p className="text-sm text-zinc-400">Readiness report unavailable.</p> : null}
      </div>
    </section>
  );
}
