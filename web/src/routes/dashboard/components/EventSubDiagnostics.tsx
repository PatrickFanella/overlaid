import type { TwitchStatus } from '../../../lib/api';
import { KeyValues, Notice, Panel, Tag } from '../ui';

export function EventSubDiagnostics({ status }: { status: TwitchStatus | null }) {
  const eventSub = status?.eventSub;

  return (
    <Panel kicker="Twitch" title="EventSub" actions={<Tag tone={eventSub?.connected ? 'ok' : 'warn'}>{eventSub?.connected ? 'connected' : 'not connected'}</Tag>}>
      <KeyValues
        items={[
          ['Chat commands', 'mods + broadcaster only'],
          ['subscriptions', eventSub ? `${eventSub.subscriptionsMade}/${eventSub.subscriptionsPlan}` : 'n/a'],
          ['reconnects', String(eventSub?.reconnectAttempts ?? 0)],
          ['last event', eventSub?.lastEventAt || 'waiting'],
          ['last disconnect', eventSub?.lastDisconnectedAt || 'none'],
        ]}
      />
      {status?.missingScopes?.length ? <Notice tone="warn">Missing scopes: {status.missingScopes.join(', ')}. Log in again to grant them.</Notice> : null}
      {eventSub?.lastError ? <Notice tone="bad">{eventSub.lastError}</Notice> : null}
    </Panel>
  );
}
