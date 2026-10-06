import type { DashboardConfig } from '../../../lib/dashboardConfig';
import { Panel } from '../ui';

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
    <Panel
      kicker="Custom CSS"
      title="OBS browser source CSS"
      description="Paste this into the Custom CSS field of an OBS browser source. It overrides overlay styles on this workstation without editing app files."
      actions={
        <button className="desk-btn desk-btn--small" type="button" onClick={() => onCopy('Custom CSS', config.customCSS)}>
          Copy CSS
        </button>
      }
    >
      <textarea
        className="desk-input desk-input--mono min-h-40"
        aria-label="Custom CSS"
        value={config.customCSS}
        onChange={(event) => updateCustomCSS(event.target.value)}
        spellCheck={false}
      />
    </Panel>
  );
}
