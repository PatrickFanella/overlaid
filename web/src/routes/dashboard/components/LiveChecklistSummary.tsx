import type { SetupChecklistItem } from '../../../lib/dashboardConfig';

type LiveChecklistSummaryProps = {
  items: SetupChecklistItem[];
};

export function LiveChecklistSummary({ items }: LiveChecklistSummaryProps) {
  const completed = items.filter((item) => item.completed).length;
  const total = items.length;

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="hud-label">Checklist</p>
          <h2 className="hud-section-title">Setup checklist summary</h2>
          <p className="hud-copy mt-1 text-xs text-zinc-300">Read-only mirror of the setup checklist.</p>
        </div>
        <span className={`hud-chip hud-chip--square ${completed === total ? 'hud-accent-emerald' : 'hud-accent-amber'}`}>
          {completed}/{total} done
        </span>
      </div>

      <div className="mt-3 grid gap-2">
        {items.map((item) => (
          <article key={item.id} className="hud-panel hud-corners hud-panel--flat hud-compact-card">
            <div className="hud-terminal-line">
              <span className="hud-terminal-key">{item.completed ? 'done' : 'open'}</span>
              <span className="hud-terminal-value font-bold">{item.title}</span>
            </div>
            {item.description ? <p className="mt-1 text-xs text-zinc-300">{item.description}</p> : null}
          </article>
        ))}
      </div>
    </section>
  );
}
