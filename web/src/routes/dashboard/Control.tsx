import { DashboardShell } from './DashboardShell';

export function DashboardControlPage() {
  return (
    <DashboardShell activePath="/dashboard/control">
      <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
        <p className="hud-label">Control</p>
        <h2 className="mt-3 text-2xl font-black text-white">Stream control</h2>
        <p className="hud-copy mt-2 text-sm text-zinc-300">Route skeleton for future event and scene controls.</p>
      </section>
    </DashboardShell>
  );
}
