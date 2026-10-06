import type { SessionBotAudit } from '../../../lib/api';
import { formatSessionBotAuditStatus } from '../../../lib/sessionReports';
import { Empty, Notice, Panel, Tag, type Tone } from '../ui';

export function SessionBotAuditPanel({ audit }: { audit: SessionBotAudit | undefined }) {
  const unavailable = !audit || Boolean(audit.loadError);
  const events = audit?.events ?? [];
  return (
    <Panel title="Session bot actions" description={unavailable ? undefined : formatSessionBotAuditStatus(audit)}>
      {unavailable ? <Notice tone="warn">{formatSessionBotAuditStatus(audit)}</Notice> : null}
      {!unavailable && events.length === 0 ? <Empty>No bot actions recorded for this session.</Empty> : null}
      {!unavailable && audit && events.length < audit.total ? (
        <Notice tone="warn">Only the newest {events.length} of {audit.total} bot actions are loaded. Exports include these loaded rows.</Notice>
      ) : null}
      {events.length > 0 ? (
        <ul className="desk-rows">
          {events.map((event) => (
            <li className="desk-row" key={event.id}>
              <div className="desk-row__line">
                <p className="desk-row__title">{event.action || event.command || event.trigger || 'bot action'}</p>
                <Tag tone={resultTone(event.result)}>{event.result}</Tag>
              </div>
              <p className="desk-row__text">{event.command || event.trigger || 'bot action'} · {event.actorLogin || event.actorRole || event.source} · {event.reason || 'no reason'}</p>
              <p className="desk-row__meta">{new Date(event.createdAt).toLocaleString()} · {event.source}</p>
            </li>
          ))}
        </ul>
      ) : null}
    </Panel>
  );
}

function resultTone(result: string): Tone {
  if (result === 'failed') return 'bad';
  if (result === 'denied' || result === 'cooldown') return 'warn';
  if (result === 'allowed') return 'ok';
  return 'neutral';
}
