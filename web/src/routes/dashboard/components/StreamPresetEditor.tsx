import type { DashboardConfig, StreamPreset } from '../../../lib/dashboardConfig';
import { Empty, Field, Panel, SubPanel } from '../ui';

type StreamPresetEditorProps = {
  config: DashboardConfig;
  presets?: StreamPreset[];
  onChange: (config: DashboardConfig) => void;
};

function parseSources(value: string) {
  return value
    .split(',')
    .map((source) => source.trim())
    .filter(Boolean);
}

function updatePreset(config: DashboardConfig, id: string, patch: Partial<StreamPreset>) {
  return {
    ...config,
    streamPresets: config.streamPresets.map((preset) => (preset.id === id ? { ...preset, ...patch } : preset)),
  };
}

export function StreamPresetEditor({ config, presets, onChange }: StreamPresetEditorProps) {
  const visiblePresets = presets ?? config.streamPresets;

  return (
    <Panel kicker="Stream presets" title="Edit scene source bundles" description="Overlay source sets for each scene, stored locally.">
      {visiblePresets.length > 0 ? (
        <div className="grid gap-3 lg:grid-cols-3">
          {visiblePresets.map((preset) => (
            <SubPanel key={preset.id} title={`Preset ${preset.id}`}>
              <Field label="Title">
                <input className="desk-input" type="text" value={preset.title} onChange={(event) => onChange(updatePreset(config, preset.id, { title: event.target.value }))} />
              </Field>

              <Field label="Sources" hint={`${preset.sources.length} source${preset.sources.length === 1 ? '' : 's'}, comma separated`}>
                <textarea className="desk-input" value={preset.sources.join(', ')} onChange={(event) => onChange(updatePreset(config, preset.id, { sources: parseSources(event.target.value) }))} />
              </Field>
            </SubPanel>
          ))}
        </div>
      ) : (
        <Empty>No stream presets for this profile.</Empty>
      )}
    </Panel>
  );
}
