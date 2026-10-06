import type { BotAuditResponse, BotStatus } from '../../../lib/api';
import { Empty, KeyValues, Notice, Panel, Tag, type Tone } from '../ui';

type BotStatusSummaryProps = {
  status: BotStatus | null;
  audit: BotAuditResponse | null;
};

export function BotStatusSummary({ status, audit }: BotStatusSummaryProps) {
  const events = audit?.events ?? [];
  const storageTone: Tone = status ? (status.available ? 'ok' : 'bad') : 'neutral';

  return (
    <div className="grid gap-4 lg:grid-cols-[0.9fr_1.1fr]">
      <Panel
        kicker="Runtime"
        title="Bot runtime"
        actions={<Tag tone={storageTone}>storage {status ? (status.available ? 'available' : 'unavailable') : 'unknown'}</Tag>}
      >
        <KeyValues
          items={[
            ['commands', String(status?.commandCount ?? 0)],
            ['timers', String(status?.timerCount ?? 0)],
            ['counters', String(status?.counterCount ?? 0)],
            ['quotes', String(status?.quoteCount ?? 0)],
            ['audit events', String(status?.auditCount ?? 0)],
          ]}
        />
        {status?.loadError ? <Notice tone="warn">Load error: {status.loadError}</Notice> : null}
        {status?.lastAuditError ? <Notice tone="bad">Audit error: {status.lastAuditError}</Notice> : null}
      </Panel>

      <Panel kicker="Audit" title="Bot audit" flush>
        {audit?.loadError ? (
          <div className="p-4">
            <Notice tone="warn">Audit unavailable: {audit.loadError}</Notice>
          </div>
        ) : null}
        <ul className="desk-rows">
          {events.length === 0 ? (
            <li className="desk-row">
              <Empty>No bot audit events yet.</Empty>
            </li>
          ) : null}
          {events.map((event) => {
            const tone = toneForResult(event.result);
            return (
              <li key={event.id} className={`desk-row ${tone === 'neutral' ? '' : `desk-row--${tone}`}`}>
                <div className="desk-row__line">
                  <p className="desk-row__title">{event.trigger || event.command || event.action || 'bot action'}</p>
                  <Tag tone={tone}>{event.result}</Tag>
                </div>
                <p className="desk-row__text">
                  {event.actorDisplayName || event.actorLogin || event.source} · {event.reason || 'no reason'}
                </p>
                <p className="desk-row__meta mono">{event.createdAt}</p>
              </li>
            );
          })}
        </ul>
      </Panel>
    </div>
  );
}

function toneForResult(result: string): Tone {
  if (result === 'failed') return 'bad';
  if (result === 'denied' || result === 'cooldown') return 'warn';
  return 'neutral';
}
