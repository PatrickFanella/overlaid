import { Panel, Tag } from '../ui';

const runbookBaseUrl = 'https://git.subcult.tv/PatrickFanella/over-laid/src/branch/main/docs/runbooks';

const recoverySteps = [
  { label: 'Readiness', text: 'Start with backend readiness. A degraded check names the failing part: OBS, Twitch, EventSub, retained state, event history, or interaction rules.' },
  { label: 'Recent events', text: 'Check event history on Sessions to see what the API recorded before an overlay or dashboard reload.' },
  { label: 'Rules and controls', text: 'Twitch rules live on Chat; deliberate operator actions live on Scenes. Viewer rules never run arbitrary OBS actions.' },
  { label: 'Package and backup', text: 'Back up and restore api-data with the runbook CLI commands. The dashboard never displays token or state archives.' },
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
    <Panel
      kicker="Recovery"
      title="Recovery console"
      description="One path for live incidents: diagnose first, then follow the matching runbook."
      actions={<Tag tone="warn">no destructive browser actions</Tag>}
    >
      <ol className="grid gap-3 md:grid-cols-2">
        {recoverySteps.map((step, index) => (
          <li key={step.label} className="desk-sub">
            <p className="desk-sub__title">
              {index + 1} · {step.label}
            </p>
            <p className="desk-row__text">{step.text}</p>
          </li>
        ))}
      </ol>

      <div className="flex flex-wrap gap-2">
        {runbookLinks.map((link) => (
          <a key={link.href} className="desk-btn desk-btn--small" href={link.href} rel="noreferrer" target="_blank">
            {link.label}
          </a>
        ))}
      </div>
    </Panel>
  );
}
