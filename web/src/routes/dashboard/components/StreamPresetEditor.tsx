import type { DashboardConfig, StreamPreset } from '../../../lib/dashboardConfig';

type StreamPresetEditorProps = {
  config: DashboardConfig;
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

export function StreamPresetEditor({ config, onChange }: StreamPresetEditorProps) {
  return (
    <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
      <p className="hud-label">Stream presets</p>
      <h2 className="mt-3 text-2xl font-black text-white">Edit scene source bundles</h2>
      <p className="hud-copy mt-2 text-sm text-zinc-300">Keep scene-specific overlay sets in local storage so preset names and source groups stay tuned to this workstation.</p>

      <div className="mt-5 grid gap-4 lg:grid-cols-3">
        {config.streamPresets.map((preset) => (
          <article key={preset.id} className="hud-panel hud-corners rounded-[1.2rem] p-4">
            <p className="hud-kicker">Preset {preset.id}</p>

            <label className="mt-4 grid gap-2 text-sm text-zinc-300">
              <span className="text-white">Title</span>
              <input
                className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4"
                type="text"
                value={preset.title}
                onChange={(event) => onChange(updatePreset(config, preset.id, { title: event.target.value }))}
              />
            </label>

            <label className="mt-4 grid gap-2 text-sm text-zinc-300">
              <span className="text-white">Sources</span>
              <textarea
                className="min-h-28 rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4"
                value={preset.sources.join(', ')}
                onChange={(event) => onChange(updatePreset(config, preset.id, { sources: parseSources(event.target.value) }))}
              />
            </label>

            <p className="mt-3 text-sm text-zinc-400">{preset.sources.length} source{preset.sources.length === 1 ? '' : 's'}</p>
          </article>
        ))}
      </div>
    </section>
  );
}
