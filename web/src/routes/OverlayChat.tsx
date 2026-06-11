import { useEffect, useMemo, useState, type CSSProperties } from 'react';
import { connectOverlayStream, getOverlayApiBaseUrl, type OverlayStreamStatus } from '../lib/eventStream';
import { formatOverlayStreamLabel, type OverlayEvent } from '../lib/events';

const maxMessages = 8;

export function OverlayChatPage() {
  const [messages, setMessages] = useState<OverlayEvent[]>([]);
  const [status, setStatus] = useState<OverlayStreamStatus>('connecting');

  useEffect(() => {
    const stream = connectOverlayStream(getOverlayApiBaseUrl(), {
      onEvent(event) {
        if (event.type === 'clear') {
          setMessages([]);
          return;
        }

        if (event.type !== 'chat_message') {
          return;
        }

        setMessages((current) => [...current, event].slice(-maxMessages));
      },
      onStatusChange(nextStatus) {
        setStatus(nextStatus);
      },
    });

    return () => {
      stream.close();
    };
  }, []);

  const streamLabel = useMemo(() => formatOverlayStreamLabel(status), [status]);

  return (
    <main className="hud-shell hud-shell--chat hud-scanlines">
      <section className="ml-0 flex w-full max-w-[34rem] flex-col gap-3">
        <header className="hud-panel hud-corners hud-accent-cyan flex items-center justify-between gap-3 rounded-[1.4rem] px-5 py-4">
          <div>
            <p className="hud-label">Twitch chat</p>
            <p className="mt-2 text-sm text-zinc-200">Lower-left feed · OBS browser source</p>
          </div>
          <span className="hud-chip">
            <span className="hud-led" />
            {streamLabel}
          </span>
        </header>

        <div role="log" aria-live="polite" aria-relevant="additions text" className="grid gap-3">
          {messages.length === 0 ? (
            <div className="hud-panel hud-corners hud-accent-cyan rounded-[1.4rem] px-5 py-4 shadow-2xl shadow-black/30">
              <p className="text-base font-semibold text-white">Waiting for Twitch chat</p>
              <p className="mt-1 text-sm text-zinc-300">EventSub chat messages will appear here.</p>
            </div>
          ) : null}

          {messages.map((message) => {
            const twitchEvent = getTwitchEvent(message);
            const name = getChatName(message);
            const color = getChatColor(twitchEvent);
            const badges = getChatBadges(twitchEvent);

            return (
              <article key={message.id} className="hud-panel hud-corners hud-chat-row rounded-[1.35rem] border-white/10 bg-black/35 px-5 py-4 shadow-xl shadow-black/30" style={color ? ({ '--hud-accent': color } as CSSProperties) : undefined}>
                <div className="flex items-start gap-3">
                  <div className="mt-1 flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl border border-white/10 bg-black/40">
                    <span className="hud-led" />
                  </div>

                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <h2 className="max-w-full truncate text-sm font-semibold uppercase tracking-[0.22em]" style={color ? { color } : undefined}>
                        {name}
                      </h2>
                      <span className="hud-badge">{formatClock(message.createdAt)}</span>
                      {badges.map((badge) => (
                        <span key={badge} className="hud-badge">
                          {badge}
                        </span>
                      ))}
                    </div>
                    <p className="mt-2 text-[0.98rem] leading-relaxed text-zinc-50">{message.message}</p>
                  </div>
                </div>
              </article>
            );
          })}
        </div>
      </section>
    </main>
  );
}

function getTwitchEvent(message: OverlayEvent) {
  const data = isRecord(message.data) ? message.data : undefined;
  return data && isRecord(data.twitchEvent) ? data.twitchEvent : undefined;
}

function getChatName(message: OverlayEvent) {
  const twitchEvent = getTwitchEvent(message);

  return (
    asString(twitchEvent?.chatter_user_name) ||
    asString(twitchEvent?.chatter_user_login) ||
    asString(twitchEvent?.user_name) ||
    message.title ||
    'chat'
  );
}

function getChatColor(twitchEvent: Record<string, unknown> | undefined) {
  const color = asString(twitchEvent?.chatter_color);
  return color || '';
}

function getChatBadges(twitchEvent: Record<string, unknown> | undefined) {
  const badges = twitchEvent?.badges;
  if (!Array.isArray(badges)) {
    return [];
  }

  return badges
    .slice(0, 3)
    .map((badge) => {
      if (!isRecord(badge)) {
        return '';
      }

      return asString(badge.set_id) || asString(badge.id) || asString(badge.version);
    })
    .filter(Boolean);
}

function formatClock(value: string) {
  return new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function asString(value: unknown) {
  return typeof value === 'string' ? value.trim() : '';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
