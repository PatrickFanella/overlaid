import type { ReactNode } from 'react';

type ConsoleSectionProps = {
  eyebrow: string;
  title?: string;
  description?: string;
  accent?: 'cyan' | 'fuchsia' | 'purple' | 'emerald' | 'amber' | 'rose';
  danger?: boolean;
  actions?: ReactNode;
  children: ReactNode;
};

export function ConsoleSection({ eyebrow, title, description, accent = 'cyan', danger = false, actions, children }: ConsoleSectionProps) {
  return (
    <section className={`hud-panel hud-corners hud-panel--flat ${danger ? 'hud-danger-zone' : `hud-accent-${accent}`} rounded-sm p-3`}>
      <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
        <div className="min-w-0">
          <p className="hud-label">{eyebrow}</p>
          {title ? <h3 className="hud-section-title">{title}</h3> : null}
          {description ? <p className="hud-copy mt-1 text-xs text-zinc-300">{description}</p> : null}
        </div>

        {actions ? <div className="flex flex-wrap gap-2 md:justify-end">{actions}</div> : null}
      </div>

      <div className="mt-3">{children}</div>
    </section>
  );
}
