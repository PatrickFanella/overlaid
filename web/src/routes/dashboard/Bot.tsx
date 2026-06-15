import { useEffect, useState } from 'react';
import { fetchBotAudit, fetchBotStatus, type BotAuditResponse, type BotStatus } from '../../lib/api';
import { loadOperatorSecret } from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { DashboardShell } from './DashboardShell';

type BotPageState = {
  status: BotStatus | null;
  audit: BotAuditResponse | null;
  loading: boolean;
  error: string;
};

export function DashboardBotPage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const operatorSecret = loadOperatorSecret();
  const [state, setState] = useState<BotPageState>({ status: null, audit: null, loading: false, error: '' });

  useEffect(() => {
    let cancelled = false;
    async function load() {
      setState((current) => ({ ...current, loading: true, error: '' }));
      try {
        const [status, audit] = await Promise.all([fetchBotStatus(apiBaseUrl, operatorSecret), fetchBotAudit(25, apiBaseUrl, operatorSecret)]);
        if (!cancelled) {
          setState({ status, audit, loading: false, error: '' });
        }
      } catch (error) {
        if (!cancelled) {
          setState((current) => ({ ...current, loading: false, error: error instanceof Error ? error.message : 'bot status unavailable' }));
        }
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, [apiBaseUrl, operatorSecret]);

  return (
    <DashboardShell activePath="/dashboard/bot">
      <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
        <p className="hud-label">Bot operations</p>
        <h2 className="hud-title hud-title--compact mt-1">FossaBot parity spine</h2>
        <p className="mt-2 max-w-3xl text-sm text-zinc-300">Bot audit is the first safety layer for future timers, custom commands, counters, quotes, channel-point workflows, and moderation tools.</p>
        {state.loading ? <p className="mt-2 text-xs text-zinc-400">Loading bot status…</p> : null}
        {state.error ? <p className="mt-2 border border-red-400/40 bg-black px-2 py-1 text-xs text-rose-200">{state.error}</p> : null}
      </section>

      <section className="grid gap-3 lg:grid-cols-[0.9fr_1.1fr]">
        <BotStatusPanel status={state.status} />
        <BotAuditPanel audit={state.audit} />
      </section>
    </DashboardShell>
  );
}

function BotStatusPanel({ status }: { status: BotStatus | null }) {
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Bot runtime</p>
      <div className="mt-2 grid gap-1">
        <Line label="storage" value={status ? (status.available ? 'available' : 'unavailable') : 'unknown'} />
        <Line label="commands" value={String(status?.commandCount ?? 0)} />
        <Line label="timers" value={String(status?.timerCount ?? 0)} />
        <Line label="audit events" value={String(status?.auditCount ?? 0)} />
      </div>
      {status?.loadError ? <p className="mt-2 border border-amber-400/40 bg-black px-2 py-1 text-xs text-amber-100">load error :: {status.loadError}</p> : null}
      {status?.lastAuditError ? <p className="mt-2 border border-red-400/40 bg-black px-2 py-1 text-xs text-rose-200">audit error :: {status.lastAuditError}</p> : null}
    </section>
  );
}

function BotAuditPanel({ audit }: { audit: BotAuditResponse | null }) {
  const events = audit?.events ?? [];
  return (
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
            <p className="mt-1 text-zinc-300">{event.actorDisplayName || event.actorLogin || event.source} · {event.reason || 'no reason'}</p>
            <p className="mt-1 text-zinc-500">{event.createdAt}</p>
          </article>
        ))}
      </div>
    </section>
  );
}

function Line({ label, value }: { label: string; value: string }) {
  return <div className="hud-terminal-line"><span className="hud-terminal-key">{label}</span><span className="hud-terminal-value">{value}</span></div>;
}
