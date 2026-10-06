import type { ControlWidget, DashboardConfig } from '../../../lib/dashboardConfig';
import { Check, Panel } from '../ui';

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
    <Panel kicker="Layout" title="Stream control modules" description="Show or hide modules, set their order, and switch between normal and compact size." flush>
      <ul className="desk-rows">
        {config.controlWidgets.map((widget) => (
          <li key={widget.id} className="desk-row">
            <div className="desk-row__line">
              <p className="desk-row__title">{widget.title}</p>
              <div className="flex flex-wrap items-center gap-4">
                <Check label={widget.enabled ? 'show' : 'hidden'} checked={widget.enabled} onChange={(checked) => updateWidget(widget.id, { enabled: checked })} />
                <label className="desk-field flex items-center gap-2">
                  <span>order</span>
                  <input
                    className="desk-input w-16"
                    type="number"
                    aria-label={`${widget.title} order`}
                    value={widget.order}
                    onChange={(event) => updateWidget(widget.id, { order: Number(event.target.value) || widget.order })}
                  />
                </label>
                <Check label="compact size" checked={widget.compact} onChange={(checked) => updateWidget(widget.id, { compact: checked })} />
              </div>
            </div>
          </li>
        ))}
      </ul>
    </Panel>
  );
}
