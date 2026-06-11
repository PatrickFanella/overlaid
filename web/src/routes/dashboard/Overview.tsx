import { DashboardShell } from './DashboardShell';

export function DashboardOverviewPage() {
  return (
    <DashboardShell activePath="/dashboard">
      <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
        <p className="hud-label">System status</p>
        <h2 className="mt-3 text-2xl font-black text-white">Dashboard overview</h2>
        <p className="hud-copy mt-2 text-sm text-zinc-300">Compact overview for the operator console.</p>
      </section>
    </DashboardShell>
  );
}
