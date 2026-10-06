import type { OBSStatus, TwitchStatus } from '../../../lib/api';
import type { DashboardConfig } from '../../../lib/dashboardConfig';
import { Panel, Tag } from '../ui';

type LivePreflightProps = {
  twitch: TwitchStatus | null;
  obs: OBSStatus | null;
  config: DashboardConfig;
  operatorSecret: string;
};

export function LivePreflight({ twitch, obs, config, operatorSecret }: LivePreflightProps) {
  const checks = [
    { label: 'Twitch authenticated', ok: Boolean(twitch?.authenticated), fix: 'Use Twitch login in the top bar.' },
    { label: 'EventSub connected', ok: Boolean(twitch?.eventSub?.connected), fix: 'Wait for EventSub, or log in again if scopes changed.' },
    { label: 'OBS reachable', ok: Boolean(obs?.reachable), fix: 'Check the OBS WebSocket server, host networking, and password.' },
    { label: 'Operator secret loaded', ok: Boolean(operatorSecret), fix: 'Enter the operator secret in Settings.', href: '/dashboard/settings' },
    { label: 'Setup checklist complete', ok: config.setupChecklist.every((item) => item.completed), fix: 'Finish the checklist on Overlays.', href: '/dashboard/overlays' },
  ];
  const passing = checks.filter((check) => check.ok).length;

  return (
    <Panel kicker="Before going live" title="Live preflight" flush actions={<Tag tone={passing === checks.length ? 'ok' : 'warn'}>{`${passing}/${checks.length}`}</Tag>}>
      <ul className="desk-rows">
        {checks.map((check) => (
          <li key={check.label} className={`desk-row desk-row--${check.ok ? 'ok' : 'bad'}`}>
            <div className="desk-row__line">
              <p className="desk-row__title">{check.label}</p>
              <span className="desk-row__meta mono">{check.ok ? 'ok' : 'fix'}</span>
            </div>
            {!check.ok ? (
              <p className="desk-row__text">
                {check.href ? <a href={check.href}>{check.fix}</a> : check.fix}
              </p>
            ) : null}
          </li>
        ))}
      </ul>
    </Panel>
  );
}
