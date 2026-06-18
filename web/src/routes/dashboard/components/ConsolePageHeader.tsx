import type { ReactNode } from 'react';

type ConsolePageHeaderProps = {
  eyebrow: string;
  title: string;
  description: string;
  accent?: 'cyan' | 'fuchsia' | 'purple' | 'emerald' | 'amber' | 'rose';
  chips?: string[];
  actions?: ReactNode;
};

export function ConsolePageHeader({ eyebrow, title, description, accent = 'cyan', chips = [], actions }: ConsolePageHeaderProps) {
  return (
    <section className={`hud-panel hud-panel--flat hud-accent-${accent} rounded-2xl p-4`}>
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div className="min-w-0">
          <p className="hud-label">{eyebrow}</p>
          <h2 className="hud-section-title">{title}</h2>
          <p className="hud-copy mt-1 text-xs text-zinc-300">{description}</p>
          {chips.length > 0 ? (
            <div className="mt-3 flex flex-wrap gap-2">
              {chips.map((chip) => (
                <span className="hud-chip hud-chip--square" key={chip}>
                  {chip}
                </span>
              ))}
            </div>
          ) : null}
        </div>

        {actions ? <div className="flex flex-wrap gap-2 lg:justify-end">{actions}</div> : null}
      </div>
    </section>
  );
}
