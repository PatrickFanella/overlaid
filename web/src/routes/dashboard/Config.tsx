import { DashboardShell } from './DashboardShell';

export function DashboardConfigPage() {
  return (
    <DashboardShell activePath="/dashboard/config">
      <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
        <p className="hud-label">Config</p>
        <h2 className="mt-3 text-2xl font-black text-white">Operator config</h2>
        <p className="hud-copy mt-2 text-sm text-zinc-300">Route skeleton for console preferences and secrets.</p>
      </section>
    </DashboardShell>
  );
}
