import { DashboardShell } from './DashboardShell';

type DashboardSetupPageProps = {
  activePath?: string;
};

export function DashboardSetupPage({ activePath = '/dashboard/setup' }: DashboardSetupPageProps = {}) {
  return (
    <DashboardShell activePath={activePath}>
      <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
        <p className="hud-label">Setup</p>
        <h2 className="mt-3 text-2xl font-black text-white">OBS setup workflow</h2>
        <p className="hud-copy mt-2 text-sm text-zinc-300">Route skeleton for stream setup and sources.</p>
      </section>
    </DashboardShell>
  );
}
