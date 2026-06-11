import { useEffect, useMemo, useState } from 'react';
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
    <main className="min-h-screen bg-transparent px-8 py-8 text-white">
      <section className="ml-auto flex max-w-xl flex-col gap-3">
        <header className="self-end rounded-full border border-white/15 bg-black/45 px-4 py-2 text-xs uppercase tracking-[0.3em] text-zinc-200 backdrop-blur-md">
          Chat · {streamLabel}
        </header>

        {messages.length === 0 ? (
          <div className="rounded-3xl border border-white/10 bg-black/35 px-5 py-4 text-right shadow-2xl shadow-black/40 backdrop-blur-md">
            <p className="text-base font-semibold">Waiting for Twitch chat</p>
            <p className="mt-1 text-xs text-zinc-300">EventSub chat messages will appear here.</p>
          </div>
        ) : null}

        {messages.map((message) => (
          <article key={message.id} className="rounded-2xl border border-cyan-200/15 bg-black/45 px-5 py-3 shadow-xl shadow-black/30 backdrop-blur-md">
            <div className="flex items-baseline gap-3">
              <h2 className="shrink-0 text-sm font-bold uppercase tracking-[0.18em] text-cyan-200">{message.title || 'chat'}</h2>
              <p className="min-w-0 text-base leading-snug text-zinc-50">{message.message}</p>
            </div>
          </article>
        ))}
      </section>
    </main>
  );
}
