import { useEffect, useMemo, useState, type CSSProperties } from 'react';
import { connectOverlayStream, getOverlayApiBaseUrl, type OverlayStreamStatus } from '../lib/eventStream';
import { formatOverlayStreamLabel, type OverlayEvent } from '../lib/events';

type BRBState = {
  active: boolean;
  title: string;
  message: string;
  eta: string;
  items: string[];
  updatedAt: string;
};

const defaultState: BRBState = {
  active: false,
  title: 'BRB overlay',
  message: 'Waiting for intermission command',
  eta: '',
  items: ['Chat stays live', 'Music continues', 'Back soon'],
  updatedAt: '',
};

export function OverlayBRBPage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const [status, setStatus] = useState<OverlayStreamStatus>('connecting');
  const [state, setState] = useState<BRBState>(defaultState);

  useEffect(() => {
    const stream = connectOverlayStream(apiBaseUrl, {
      onStatusChange: setStatus,
      onEvent: (event) => {
        if (event.type === 'clear') {
          setState(defaultState);
          return;
        }
        if (event.type === 'brb') {
          setState(brbStateFromEvent(event));
        }
      },
    });

    return () => stream.close();
  }, [apiBaseUrl]);

  const statusLabel = formatOverlayStreamLabel(status);
  const items = useMemo(() => (state.items.length ? state.items : defaultState.items), [state.items]);

  return (
    <main className="hud-shell hud-shell--compact hud-scanlines text-white">
      <section className={`hud-panel hud-corners mx-auto grid w-full max-w-5xl gap-5 p-6 ${state.active ? 'hud-accent-rose' : 'hud-accent-cyan'}`}>
        <header className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <p className="hud-label">/overlay/brb</p>
            <p className="hud-kicker mt-3">BRB overlay</p>
            <h1 className="mt-3 text-[clamp(3rem,9vw,8rem)] font-black leading-none tracking-[-0.08em] text-white">{state.active ? state.title : 'STANDBY'}</h1>
            <p className="mt-4 max-w-3xl text-[clamp(1rem,2vw,1.5rem)] font-semibold text-zinc-100">{state.message}</p>
          </div>
          <div className="grid gap-2 text-right">
            <span className="hud-chip">
              <span className={`hud-led ${status === 'live' ? '' : 'hud-led--warn'}`} />
              {statusLabel}
            </span>
            <span className="hud-chip">{state.active ? 'intermission live' : 'waiting'}</span>
          </div>
        </header>

        <div className="grid gap-4 lg:grid-cols-[1fr_0.75fr]">
          <section className="hud-panel hud-corners p-5">
            <p className="hud-kicker">Return window</p>
            <p className="mt-3 text-[clamp(2rem,5vw,4.5rem)] font-black tracking-[-0.05em] text-white">{state.eta || 'SOON'}</p>
            <p className="mt-4 text-sm uppercase tracking-[0.26em] text-zinc-400">{state.updatedAt ? `updated ${formatTime(state.updatedAt)}` : 'dashboard controlled'}</p>
          </section>

          <section className="grid gap-3" aria-label="Intermission notes">
            {items.map((item, index) => (
              <div key={`${item}-${index}`} className="hud-panel hud-corners p-4" style={{ '--hud-accent': ['var(--hud-rose)', 'var(--hud-amber)', 'var(--hud-cyan)'][index % 3] } as CSSProperties}>
                <p className="hud-kicker">{String(index + 1).padStart(2, '0')}</p>
                <p className="mt-2 text-lg font-semibold text-white">{item}</p>
              </div>
            ))}
          </section>
        </div>
      </section>
    </main>
  );
}

function brbStateFromEvent(event: OverlayEvent): BRBState {
  const data = event.data || {};
  const active = asBoolean(data.active, true);
  return {
    active,
    title: active ? event.title || asString(data.title) || 'Be right back' : defaultState.title,
    message: active ? event.message || asString(data.message) || 'Short intermission. Stream resumes soon.' : defaultState.message,
    eta: asString(data.eta),
    items: asStringArray(data.items),
    updatedAt: event.createdAt,
  };
}

function asString(value: unknown) {
  return typeof value === 'string' ? value.trim() : '';
}

function asBoolean(value: unknown, fallback: boolean) {
  return typeof value === 'boolean' ? value : fallback;
}

function asStringArray(value: unknown) {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter((item): item is string => typeof item === 'string' && item.trim().length > 0).map((item) => item.trim()).slice(0, 5);
}

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return 'just now';
  }
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}
