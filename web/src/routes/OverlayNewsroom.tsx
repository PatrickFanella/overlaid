import { useEffect, useState } from 'react';
import { getOverlayApiBaseUrl, type OverlayStreamStatus } from '../lib/eventStream';
import { FisheyeMark, Kicker, OverlayStage } from '../overlay/Brand';

type BroadcastCard = { current: { title: string; publisher: string; kind: string; warning?:string } | null; question: { text: string; askedBy: string } | null };
export function OverlayNewsroomPage() {
  const [card, setCard] = useState<BroadcastCard>({ current: null, question: null });
  const [status, setStatus] = useState<OverlayStreamStatus>('connecting');
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    if (params.get('demo') === '1') { setCard({ current: { title: '[Demo] Community radio opens a new studio', publisher: 'example.org', kind: 'article' }, question: null }); setStatus('live'); return; }
    const endpoint = new URL('/api/newsroom/overlay', getOverlayApiBaseUrl()); const key = params.get('key'); if (key) endpoint.searchParams.set('key', key);
    let cancelled = false; let running = false;
    async function load() { if (running) return; running = true; try { const response = await fetch(endpoint, { cache: 'no-store', signal: AbortSignal.timeout(5000) }); if (!response.ok) throw new Error('Unavailable'); const next = await response.json() as BroadcastCard; if (!cancelled) { setCard(next); setStatus('live'); } } catch { if (!cancelled) { setCard({ current: null, question: null }); setStatus('error'); } } finally { running = false; } }
    void load(); const interval = window.setInterval(() => { void load(); }, 3000); return () => { cancelled = true; window.clearInterval(interval); };
  }, []);
  const text = card.question?.text ?? card.current?.title;
  return <OverlayStage name="newsroom" status={status} label="Newsroom card">{text ? <section className="sc-newsroom"><FisheyeMark size={76} /><div><Kicker>{card.question ? `Question · ${card.question.askedBy || 'Audience'}` : card.current?.publisher}</Kicker><h1 className="sc-display">{text}</h1>{!card.question && card.current?.warning ? <p className="sc-kicker">Content warning: {card.current.warning}</p> : null}</div></section> : <div className="sc-standby"><h1 className="sc-standby__title">Newsroom card</h1><p className="sc-standby__hint">{status === 'error' ? 'Newsroom connection unavailable' : 'Waiting for a link or audience question'}</p></div>}</OverlayStage>;
}
