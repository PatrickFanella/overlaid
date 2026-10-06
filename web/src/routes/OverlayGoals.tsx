import { useState, type CSSProperties } from 'react';
import type { OverlayEvent } from '../lib/events';
import { useOverlayStream } from '../lib/useOverlayStream';
import { Kicker, OverlayStage } from '../overlay/Brand';
import { asNumber, asString, isRecord } from '../overlay/data';

type Goal = {
  id: string;
  label: string;
  current: number;
  target: number;
  unit: string;
  note: string;
};

export function OverlayGoalsPage() {
  const [goals, setGoals] = useState<Goal[]>([]);

  const status = useOverlayStream((event) => {
    if (event.type === 'clear') {
      setGoals([]);
      return;
    }

    if (event.data?.retainedStateKey && event.data.retainedStateKey !== 'goals') {
      return;
    }

    const nextGoals = goalsFromEvent(event);
    if (nextGoals.length > 0) {
      setGoals((current) => mergeGoals(current, nextGoals));
    }
  });

  const [primary, ...rest] = goals;

  return (
    <OverlayStage name="goals" status={status} label="Goal rail">
      <section className="sc-goals">
        {primary ? (
          <article key={primary.id} className="sc-goal">
            <Kicker>
              goal <span className="sc-label--sep">{primary.note}</span>
            </Kicker>
            <h1 className="sc-display sc-goal__label">{primary.label}</h1>
            <div className="sc-goal__figures">
              <span className="sc-display sc-goal__current">{formatNumber(primary.current)}</span>
              <span className="sc-display sc-goal__target">
                / {formatNumber(primary.target)} {primary.unit}
              </span>
              <span className="sc-display sc-goal__percent">{goalPercent(primary)}%</span>
            </div>
            <Meter percent={goalPercent(primary)} label={`${primary.label} progress`} />
          </article>
        ) : (
          <div className="sc-standby">
            <p className="sc-standby__title">Waiting for goal events</p>
            <p className="sc-standby__hint">Goal rail · send data.goal or data.goals</p>
          </div>
        )}

        {rest.map((goal) => (
          <article key={goal.id} className="sc-goal-row">
            <h2 className="sc-display">{goal.label}</h2>
            <span className="sc-label">
              {formatNumber(goal.current)} / {formatNumber(goal.target)} {goal.unit}
            </span>
            <Meter percent={goalPercent(goal)} label={`${goal.label} progress`} />
          </article>
        ))}
      </section>
    </OverlayStage>
  );
}

function Meter({ percent, label }: { percent: number; label: string }) {
  return (
    <div className="sc-meter" role="progressbar" aria-label={label} aria-valuenow={percent} aria-valuemin={0} aria-valuemax={100} style={{ '--meter': `${percent}%` } as CSSProperties}>
      <div className="sc-meter__fill" />
    </div>
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

  const current = asNumber(raw.current ?? raw.value);
  const target = asNumber(raw.target ?? raw.total);
  if (!Number.isFinite(current) || !Number.isFinite(target) || target <= 0) {
    return null;
  }

  return {
    id: asString(raw.id) || `${event.source || 'goal'}-${index}`,
    label: asString(raw.label) || asString(raw.title) || event.title || 'Goal',
    current,
    target,
    unit: asString(raw.unit) || 'items',
    note: asString(raw.note) || 'live',
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
