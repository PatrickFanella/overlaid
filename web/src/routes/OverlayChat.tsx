import { useState, type CSSProperties } from 'react';
import { loadDashboardConfig } from '../lib/dashboardConfig';
import type { OverlayEvent } from '../lib/events';
import { useOverlayStream } from '../lib/useOverlayStream';
import { OverlayStage } from '../overlay/Brand';
import { asString, isRecord } from '../overlay/data';

export function OverlayChatPage() {
  const [display] = useState(() => loadDashboardConfig().display);
  const [messages, setMessages] = useState<OverlayEvent[]>([]);

  const status = useOverlayStream((event) => {
    if (event.type === 'clear') {
      setMessages([]);
      return;
    }

    if (event.type !== 'chat_message') {
      return;
    }

    setMessages((current) => [...current, event].slice(-display.chatMaxVisible));
  });

  return (
    <OverlayStage name="chat" status={status} label="Twitch chat">
      <div role="log" aria-live="polite" aria-relevant="additions text" className="sc-chat-log">
        {messages.length === 0 ? (
          <div className="sc-standby">
            <p className="sc-standby__title">Waiting for Twitch chat</p>
            <p className="sc-standby__hint">EventSub chat messages appear here</p>
          </div>
        ) : null}

        {messages.map((message) => {
          const twitchEvent = getTwitchEvent(message);
          const color = asString(twitchEvent?.chatter_color);

          return (
            <article key={message.id} className="sc-chat" style={color ? ({ '--chatter': color } as CSSProperties) : undefined}>
              <header className="sc-chat__head">
                <span className="sc-chat__swatch" aria-hidden="true" />
                <h2 className="sc-chat__name">{getChatName(message, twitchEvent)}</h2>
                {getChatBadges(twitchEvent).map((badge) => (
                  <span key={badge} className="sc-label sc-chat__badge">
                    {badge}
                  </span>
                ))}
              </header>
              <p className="sc-chat__text">{message.message}</p>
            </article>
          );
        })}
      </div>
    </OverlayStage>
  );
}

function getTwitchEvent(message: OverlayEvent) {
  const data = isRecord(message.data) ? message.data : undefined;
  return data && isRecord(data.twitchEvent) ? data.twitchEvent : undefined;
}

function getChatName(message: OverlayEvent, twitchEvent: Record<string, unknown> | undefined) {
  return (
    asString(twitchEvent?.chatter_user_name) ||
    asString(twitchEvent?.chatter_user_login) ||
    asString(twitchEvent?.user_name) ||
    message.title ||
    'chat'
  );
}

const badgeLabels: Record<string, string> = {
  broadcaster: 'host',
  moderator: 'mod',
  vip: 'vip',
  subscriber: 'sub',
  founder: 'founder',
};

function getChatBadges(twitchEvent: Record<string, unknown> | undefined) {
  const badges = twitchEvent?.badges;
  if (!Array.isArray(badges)) {
    return [];
  }

  return badges
    .map((badge) => (isRecord(badge) ? asString(badge.set_id) || asString(badge.id) : ''))
    .map((id) => badgeLabels[id] ?? '')
    .filter(Boolean)
    .slice(0, 2);
}
