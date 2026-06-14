const runbookBaseUrl = 'https://git.subcult.tv/PatrickFanella/over-laid/src/branch/main/docs/runbooks';

const recoverySteps = [
  {
    label: '1 · Readiness',
    text: 'Start with Backend readiness. Degraded checks identify OBS, Twitch, EventSub, retained state, event history, or interaction-rule failures.',
  },
  {
    label: '2 · Recent events',
    text: 'Use Event history to confirm what the API recorded before an overlay or dashboard reload.',
  },
  {
    label: '3 · Rules and controls',
    text: 'Use Config for Twitch interaction rules and Control for deliberate operator actions. Viewer rules never run arbitrary OBS actions.',
  },
  {
    label: '4 · Package and backup',
    text: 'Use CLI runbook commands for api-data backups and restores. The dashboard never displays token/state archives.',
  },
] as const;

const runbookLinks = [
  { href: `${runbookBaseUrl}/production-recovery.md`, label: 'Production recovery' },
  { href: `${runbookBaseUrl}/production-packaging.md`, label: 'Packaging + backups' },
  { href: `${runbookBaseUrl}/event-history.md`, label: 'Event history' },
  { href: `${runbookBaseUrl}/twitch-interaction-rules.md`, label: 'Twitch rules' },
  { href: `${runbookBaseUrl}/live-stream-preflight.md`, label: 'Live preflight' },
] as const;

export function RecoveryConsolePanel() {
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="hud-label">recovery</p>
          <h2 className="hud-section-title">Recovery console</h2>
          <p className="hud-copy mt-1 text-xs text-zinc-300">
            One operator path for live incidents: diagnose first, then recover from the relevant runbook.
          </p>
        </div>
        <span className="hud-chip hud-chip--square hud-accent-amber">no destructive browser actions</span>
      </div>

      <div className="mt-4 grid gap-2 md:grid-cols-2">
        {recoverySteps.map((step) => (
          <article key={step.label} className="hud-panel hud-corners hud-panel--flat hud-compact-card">
            <div className="hud-terminal-line">
              <span className="hud-terminal-key">step</span>
              <span className="hud-terminal-value font-bold">{step.label}</span>
            </div>
            <p className="mt-2 text-xs text-zinc-300">{step.text}</p>
          </article>
        ))}
      </div>

      <div className="mt-4 flex flex-wrap gap-2">
        {runbookLinks.map((link) => (
          <a key={link.href} className="hud-action-button hud-action-button--compact" href={link.href} rel="noreferrer" target="_blank">
            {link.label}
          </a>
        ))}
      </div>
    </section>
  );
}
