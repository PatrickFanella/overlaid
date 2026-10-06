import type { AutomationProfileWarning } from '../../lib/automationProfiles';
import type { AutomationProfile } from '../../lib/dashboardConfig';
import { Notice, Tag } from './ui';

export function ActiveProfileTag({ profile }: { profile: AutomationProfile }) {
  return (
    <a href="/dashboard/settings#automation-profiles" title={`${profile.description} Change it in Settings.`}>
      <Tag>Active profile · {profile.title}</Tag>
    </a>
  );
}

export function ProfileWarnings({ warnings }: { warnings: AutomationProfileWarning[] }) {
  if (warnings.length === 0) {
    return null;
  }
  return (
    <Notice tone="warn">
      <ul>
        {warnings.map((warning) => (
          <li key={`${warning.profileId}:${warning.message}`}>{warning.message}</li>
        ))}
      </ul>
    </Notice>
  );
}

export function MissingSecretNotice({ operatorSecret }: { operatorSecret: string }) {
  if (operatorSecret) {
    return null;
  }
  return (
    <Notice tone="warn">
      Operator secret missing. Protected actions stay disabled until you add it in <a href="/dashboard/settings">Settings</a>.
    </Notice>
  );
}
