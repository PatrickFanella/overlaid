import type { ControlWidget, DashboardConfig } from '../../../lib/dashboardConfig';

type ControlLayoutEditorProps = {
  config: DashboardConfig;
  onChange: (config: DashboardConfig) => void;
};

export function ControlLayoutEditor({ config, onChange }: ControlLayoutEditorProps) {
  function updateWidget(id: string, patch: Partial<ControlWidget>) {
    onChange({
      ...config,
      controlWidgets: config.controlWidgets.map((widget) => (widget.id === id ? { ...widget, ...patch } : widget)),
    });
  }

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex items-center justify-between gap-2">
        <div>
          <p className="hud-label">Layout</p>
          <h2 className="hud-section-title">Stream control modules</h2>
          <p className="mt-1 text-xs text-zinc-400">Show/hide modules, move them by order, and resize between normal/compact.</p>
        </div>
        <span className="hud-chip hud-chip--square">configurable</span>
      </div>

      <div className="mt-3 grid gap-2 md:grid-cols-2 xl:grid-cols-5">
        {config.controlWidgets.map((widget) => (
          <article key={widget.id} className="hud-panel hud-corners hud-panel--flat hud-compact-card">
            <label className="flex items-center gap-2 text-xs font-bold uppercase tracking-[0.16em] text-white">
              <input className="accent-cyan-300" type="checkbox" checked={widget.enabled} onChange={(event) => updateWidget(widget.id, { enabled: event.target.checked })} />
              {widget.enabled ? 'show' : 'hidden'} · {widget.title}
            </label>
            <div className="mt-2 grid grid-cols-2 gap-2">
              <label className="grid gap-1 text-xs text-zinc-300">
                order
                <input className="hud-input" type="number" value={widget.order} onChange={(event) => updateWidget(widget.id, { order: Number(event.target.value) || widget.order })} />
              </label>
              <label className="flex items-end gap-2 pb-2 text-xs text-zinc-300">
                <input className="accent-cyan-300" type="checkbox" checked={widget.compact} onChange={(event) => updateWidget(widget.id, { compact: event.target.checked })} />
                compact size
              </label>
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}
