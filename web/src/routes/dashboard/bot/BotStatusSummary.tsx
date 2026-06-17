import type { BotAuditResponse, BotStatus } from '../../../lib/api';

type BotStatusSummaryProps = {
  status: BotStatus | null;
  audit: BotAuditResponse | null;
};

export function BotStatusSummary({ status, audit }: BotStatusSummaryProps) {
  const events = audit?.events ?? [];

  return (
    <section className="grid gap-3 lg:grid-cols-[0.9fr_1.1fr]">
      <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
        <p className="hud-label">Bot runtime</p>
        <div className="mt-2 grid gap-1">
          <Line label="storage" value={status ? (status.available ? 'available' : 'unavailable') : 'unknown'} />
          <Line label="commands" value={String(status?.commandCount ?? 0)} />
          <Line label="timers" value={String(status?.timerCount ?? 0)} />
          <Line label="counters" value={String(status?.counterCount ?? 0)} />
          <Line label="quotes" value={String(status?.quoteCount ?? 0)} />
          <Line label="audit events" value={String(status?.auditCount ?? 0)} />
        </div>
        {status?.loadError ? <p className="mt-2 border border-amber-400/40 bg-black px-2 py-1 text-xs text-amber-100">load error :: {status.loadError}</p> : null}
        {status?.lastAuditError ? <p className="mt-2 border border-red-400/40 bg-black px-2 py-1 text-xs text-rose-200">audit error :: {status.lastAuditError}</p> : null}
      </section>

      <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
        <p className="hud-label">Bot audit</p>
        {audit?.loadError ? <p className="mt-2 border border-amber-400/40 bg-black px-2 py-1 text-xs text-amber-100">audit unavailable :: {audit.loadError}</p> : null}
        {events.length === 0 ? <p className="mt-2 text-sm text-zinc-400">No bot audit events yet.</p> : null}
        <div className="mt-2 grid gap-2">
          {events.map((event) => (
            <article key={event.id} className="border border-zinc-700 bg-black/40 p-2 text-xs">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <strong className="text-cyan-100">{event.trigger || event.command || event.action || 'bot action'}</strong>
                <span className="hud-chip hud-chip--square">{event.result}</span>
              </div>
              <p className="mt-1 text-zinc-300">
                {event.actorDisplayName || event.actorLogin || event.source} · {event.reason || 'no reason'}
              </p>
              <p className="mt-1 text-zinc-500">{event.createdAt}</p>
            </article>
          ))}
        </div>
      </section>
    </section>
  );
}

function Line({ label, value }: { label: string; value: string }) {
  return (
    <div className="hud-terminal-line">
      <span className="hud-terminal-key">{label}</span>
      <span className="hud-terminal-value">{value}</span>
    </div>
  );
}
