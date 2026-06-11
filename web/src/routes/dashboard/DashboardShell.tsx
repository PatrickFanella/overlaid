import type { ReactNode } from 'react';

const dashboardLinks = [
  ['/dashboard', 'Overview'],
  ['/dashboard/setup', 'Setup'],
  ['/dashboard/sources', 'Sources'],
  ['/dashboard/control', 'Stream control'],
  ['/dashboard/config', 'Config'],
] as const;

type DashboardShellProps = {
  activePath: string;
  children: ReactNode;
};

export function DashboardShell({ activePath, children }: DashboardShellProps) {
  return (
    <main className="hud-shell hud-scanlines min-h-screen bg-[radial-gradient(circle_at_top_left,rgba(103,232,249,0.14),transparent_28%),radial-gradient(circle_at_top_right,rgba(244,114,182,0.12),transparent_34%),linear-gradient(135deg,#020617,#0b1020_54%,#020305)] px-6 py-8 text-zinc-100">
      <section className="mx-auto flex max-w-7xl flex-col gap-6">
        <header className="hud-panel hud-corners rounded-[1.8rem] p-5">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
            <div>
              <p className="hud-label">over-laid command center</p>
              <h1 className="hud-title mt-3">Dashboard</h1>
            </div>

            <nav className="flex flex-wrap gap-2" aria-label="Dashboard sections">
              {dashboardLinks.map(([href, label]) => (
                <a
                  key={href}
                  className={`hud-chip ${activePath === href ? 'border-cyan-200/60 text-cyan-100' : ''}`}
                  href={href}
                >
                  {label}
                </a>
              ))}
            </nav>
          </div>
        </header>

        {children}
      </section>
    </main>
  );
}
