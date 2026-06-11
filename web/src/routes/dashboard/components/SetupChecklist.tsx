import { useState } from 'react';
import type { DashboardConfig } from '../../../lib/dashboardConfig';

type SetupChecklistProps = {
  config: DashboardConfig;
  onChange: (config: DashboardConfig) => void;
};

export function SetupChecklist({ config, onChange }: SetupChecklistProps) {
  const [showCompleted, setShowCompleted] = useState(false);
  const allComplete = config.setupChecklist.every((item) => item.completed);
  const visibleItems = config.setupChecklist.filter((item) => !item.completed || showCompleted);

  function toggle(id: string) {
    onChange({
      ...config,
      setupChecklist: config.setupChecklist.map((item) => (item.id === id ? { ...item, completed: !item.completed } : item)),
    });
  }

  return (
    <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
      <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
        <div>
          <p className="hud-label">OBS setup workflow</p>
          <h2 className="mt-3 text-2xl font-black text-white">Checklist</h2>
          <p className="hud-copy mt-2 text-sm text-zinc-300">Track OBS onboarding, smoke tests, and setup decisions locally in this browser.</p>
        </div>

        <button
          className="hud-chip"
          type="button"
          onClick={() => setShowCompleted((current) => !current)}
        >
          {showCompleted ? 'Hide completed' : 'Show completed'}
        </button>
      </div>

      {allComplete ? (
        <p className="mt-5 rounded-2xl border border-emerald-300/30 bg-emerald-300/10 px-4 py-3 text-sm text-emerald-100">
          Setup complete. All workflow items are green.
        </p>
      ) : null}

      <div className="mt-5 grid gap-3">
        {visibleItems.map((item) => (
          <label key={item.id} className="hud-panel hud-corners flex cursor-pointer gap-3 rounded-[1.2rem] p-4">
            <input
              className="mt-1 size-4 accent-cyan-300"
              type="checkbox"
              checked={item.completed}
              onChange={() => toggle(item.id)}
            />
            <span>
              <span className="block font-semibold text-white">{item.title}</span>
              <span className="mt-1 block text-sm text-zinc-300">{item.description}</span>
            </span>
          </label>
        ))}
      </div>
    </section>
  );
}
