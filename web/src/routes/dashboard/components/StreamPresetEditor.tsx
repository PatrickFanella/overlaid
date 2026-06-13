import type { DashboardConfig, StreamPreset } from '../../../lib/dashboardConfig';

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
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Stream presets</p>
      <h2 className="hud-section-title">Edit scene source bundles</h2>
      <p className="hud-copy mt-1 text-xs text-zinc-300">Scene-specific overlay sets stored locally.</p>

      <div className="mt-3 grid gap-2 lg:grid-cols-3">
        {visiblePresets.map((preset) => (
          <article key={preset.id} className="hud-panel hud-corners hud-panel--flat hud-compact-card">
            <p className="hud-kicker">Preset {preset.id}</p>

            <label className="mt-2 grid gap-1 text-xs text-zinc-300">
              <span className="text-white">Title</span>
              <input
                className="hud-input"
                type="text"
                value={preset.title}
                onChange={(event) => onChange(updatePreset(config, preset.id, { title: event.target.value }))}
              />
            </label>

            <label className="mt-2 grid gap-1 text-xs text-zinc-300">
              <span className="text-white">Sources</span>
              <textarea
                className="hud-input min-h-20"
                value={preset.sources.join(', ')}
                onChange={(event) => onChange(updatePreset(config, preset.id, { sources: parseSources(event.target.value) }))}
              />
            </label>

            <p className="mt-2 text-xs text-zinc-400">{preset.sources.length} source{preset.sources.length === 1 ? '' : 's'}</p>
          </article>
        ))}
      </div>
    </section>
  );
}
