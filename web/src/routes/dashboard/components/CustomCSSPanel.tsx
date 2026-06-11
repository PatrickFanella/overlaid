import type { DashboardConfig } from '../../../lib/dashboardConfig';

type CustomCSSPanelProps = {
  config: DashboardConfig;
  onChange: (config: DashboardConfig) => void;
  onCopy: (label: string, value: string) => void;
};

export function CustomCSSPanel({ config, onChange, onCopy }: CustomCSSPanelProps) {
  function updateCustomCSS(customCSS: string) {
    onChange({ ...config, customCSS });
  }

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex items-center justify-between gap-2">
        <div>
          <p className="hud-label">Custom CSS</p>
          <h2 className="hud-section-title">OBS browser source CSS</h2>
        </div>
        <button className="hud-chip hud-chip--square" type="button" onClick={() => onCopy('Custom CSS', config.customCSS)}>
          Copy CSS
        </button>
      </div>

      <textarea
        className="hud-input hud-input--mono mt-3 min-h-40"
        value={config.customCSS}
        onChange={(event) => updateCustomCSS(event.target.value)}
        spellCheck={false}
      />
      <p className="mt-2 text-xs leading-5 text-zinc-400">Paste this into OBS Browser Source → Custom CSS. This gives each browser source a per-workstation CSS override without editing app files.</p>
    </section>
  );
}
