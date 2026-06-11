import { useEffect, useMemo, useState, type CSSProperties } from 'react';
import { connectOverlayStream, getOverlayApiBaseUrl, type OverlayStreamStatus } from '../lib/eventStream';
import { formatOverlayStreamLabel, type OverlayEvent } from '../lib/events';

type Goal = {
  id: string;
  label: string;
  current: number;
  target: number;
  unit: string;
  note: string;
  accent: string;
};

const standbyGoal: Goal = { id: 'standby', label: 'Waiting for goal events', current: 0, target: 1, unit: 'items', note: 'send data.goal or data.goals', accent: 'var(--hud-emerald)' };

export function OverlayGoalsPage() {
  const [goals, setGoals] = useState<Goal[]>([]);
  const [lastEvent, setLastEvent] = useState<OverlayEvent | null>(null);
  const [status, setStatus] = useState<OverlayStreamStatus>('connecting');

  useEffect(() => {
    const stream = connectOverlayStream(getOverlayApiBaseUrl(), {
      onEvent(event) {
        if (event.type === 'clear') {
          setGoals([]);
          setLastEvent(null);
          return;
        }

        if (event.data?.retainedStateKey && event.data.retainedStateKey !== 'goals') {
          return;
        }

        const nextGoals = goalsFromEvent(event);
        if (nextGoals.length === 0) {
          if (event.data?.retainedStateKey) {
            return;
          }
          if (event.type === 'alert' || event.type === 'service_status') {
            setLastEvent(event);
          }
          return;
        }

        setLastEvent(event);
        setGoals((current) => mergeGoals(current, nextGoals));
      },
      onStatusChange(nextStatus) {
        setStatus(nextStatus);
      },
    });

    return () => stream.close();
  }, []);

  const streamLabel = useMemo(() => formatOverlayStreamLabel(status), [status]);
  const primary = goals[0] ?? standbyGoal;

  return (
    <main className="hud-shell hud-shell--compact hud-scanlines text-white">
      <section className="mx-auto grid w-full max-w-5xl gap-4 lg:grid-cols-[1.2fr_0.8fr]">
        <div className="hud-panel hud-corners hud-accent-emerald rounded-[1.4rem] p-6" style={{ '--hud-accent': primary.accent } as CSSProperties}>
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div>
              <p className="hud-label">Goal rail</p>
              <h1 className="mt-3 text-[clamp(2.3rem,5vw,4.5rem)] font-black leading-none tracking-[-0.06em] text-white">{primary.label}</h1>
            </div>
            <span className="hud-chip">
              <span className="hud-led" />
              {streamLabel}
            </span>
          </div>

          <div className="mt-7">
            <div className="flex items-end justify-between gap-4">
              <p className="text-[clamp(2.2rem,7vw,6rem)] font-black leading-none tracking-[-0.08em] text-white">{formatNumber(primary.current)}</p>
              <p className="hud-kicker pb-3 text-right">of {formatNumber(primary.target)} {primary.unit}</p>
            </div>
            <div className="mt-4 hud-progress hud-progress--large" style={{ '--hud-progress': `${goalPercent(primary)}%` } as CSSProperties}>
              <div className="hud-progress__fill" />
            </div>
            <div className="mt-3 flex items-center justify-between text-xs uppercase tracking-[0.24em] text-zinc-400">
              <span>{goalPercent(primary)}% complete</span>
              <span>{primary.note}</span>
            </div>
          </div>
        </div>

        <aside className="grid gap-3">
          {goals.slice(1, 4).map((goal) => (
            <article key={goal.id} className="hud-panel hud-corners rounded-[1.25rem] p-4" style={{ '--hud-accent': goal.accent } as CSSProperties}>
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="hud-kicker">{goal.label}</p>
                  <p className="mt-2 text-lg font-semibold text-white">{formatNumber(goal.current)} / {formatNumber(goal.target)} {goal.unit}</p>
                </div>
                <span className="hud-badge">{goalPercent(goal)}%</span>
              </div>
              <div className="mt-3 hud-progress" style={{ '--hud-progress': `${goalPercent(goal)}%` } as CSSProperties}>
                <div className="hud-progress__fill" />
              </div>
            </article>
          ))}

          <article className="hud-panel hud-corners hud-accent-cyan rounded-[1.25rem] p-4">
            <p className="hud-kicker">Latest signal</p>
            <p className="mt-2 text-lg font-semibold text-white">{lastEvent?.title || 'Waiting for goal events'}</p>
            <p className="mt-2 text-sm leading-6 text-zinc-300">{lastEvent?.message || 'Send an SSE event with data.goal or data.goals to update this browser source.'}</p>
          </article>
        </aside>
      </section>
    </main>
  );
}

function goalsFromEvent(event: OverlayEvent) {
  const data = isRecord(event.data) ? event.data : undefined;
  const rawGoals = Array.isArray(data?.goals) ? data.goals : isRecord(data?.goal) ? [data.goal] : [];

  return rawGoals.map((raw, index) => goalFromRecord(raw, event, index)).filter((goal): goal is Goal => Boolean(goal));
}

function goalFromRecord(raw: unknown, event: OverlayEvent, index: number): Goal | null {
  if (!isRecord(raw)) {
    return null;
  }

  const id = asString(raw.id) || `${event.source || 'goal'}-${index}`;
  const current = asNumber(raw.current ?? raw.value);
  const target = asNumber(raw.target ?? raw.total);
  if (!Number.isFinite(current) || !Number.isFinite(target) || target <= 0) {
    return null;
  }

  return {
    id,
    label: asString(raw.label) || asString(raw.title) || event.title || 'Goal',
    current,
    target,
    unit: asString(raw.unit) || 'items',
    note: asString(raw.note) || event.message || 'live update',
    accent: asString(raw.accent) || accentForIndex(index),
  };
}

function mergeGoals(current: Goal[], next: Goal[]) {
  const byID = new Map(current.map((goal) => [goal.id, goal]));
  next.forEach((goal) => byID.set(goal.id, goal));
  return Array.from(byID.values()).slice(0, 4);
}

function goalPercent(goal: Goal) {
  return Math.max(0, Math.min(100, Math.round((goal.current / goal.target) * 100)));
}

function formatNumber(value: number) {
  return new Intl.NumberFormat().format(value);
}

function accentForIndex(index: number) {
  return ['var(--hud-emerald)', 'var(--hud-fuchsia)', 'var(--hud-cyan)', 'var(--hud-amber)'][index % 4];
}

function asString(value: unknown) {
  return typeof value === 'string' ? value.trim() : '';
}

function asNumber(value: unknown) {
  if (typeof value === 'number') {
    return value;
  }
  if (typeof value === 'string' && value.trim()) {
    return Number(value);
  }
  return Number.NaN;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
