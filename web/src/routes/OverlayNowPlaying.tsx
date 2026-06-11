import { useEffect, useMemo, useState, type CSSProperties } from 'react';
import { connectOverlayStream, getOverlayApiBaseUrl, type OverlayStreamStatus } from '../lib/eventStream';
import { formatOverlayStreamLabel, type OverlayEvent } from '../lib/events';

type Track = {
  player: string;
  title: string;
  artist: string;
  album: string;
  artUrl: string;
  playbackStatus: string;
  positionMs: number;
  lengthMs: number;
};

const fallbackTrack: Track = {
  player: 'mpris',
  title: 'Waiting for MPRIS',
  artist: 'Start a local media player',
  album: 'Now-playing overlay',
  artUrl: '',
  playbackStatus: 'Stopped',
  positionMs: 0,
  lengthMs: 0,
};

export function OverlayNowPlayingPage() {
  const [track, setTrack] = useState<Track>(fallbackTrack);
  const [status, setStatus] = useState<OverlayStreamStatus>('connecting');

  useEffect(() => {
    const stream = connectOverlayStream(getOverlayApiBaseUrl(), {
      onEvent(event) {
        if (event.type === 'clear') {
          setTrack(fallbackTrack);
          return;
        }

        const next = trackFromEvent(event);
        if (next) {
          setTrack(next);
        }
      },
      onStatusChange(nextStatus) {
        setStatus(nextStatus);
      },
    });

    return () => stream.close();
  }, []);

  const streamLabel = useMemo(() => formatOverlayStreamLabel(status), [status]);
  const percent = track.lengthMs > 0 ? Math.max(0, Math.min(100, Math.round((track.positionMs / track.lengthMs) * 100))) : 0;

  return (
    <main className="hud-shell hud-shell--compact hud-scanlines text-white">
      <section className="hud-panel hud-corners hud-accent-cyan mx-auto grid w-full max-w-5xl gap-5 rounded-[1.4rem] p-6 lg:grid-cols-[220px_1fr] lg:items-center">
        <div className="flex items-center justify-center rounded-[1.4rem] border border-white/10 bg-[radial-gradient(circle_at_top_left,rgba(103,232,249,0.35),transparent_42%),linear-gradient(160deg,rgba(255,255,255,0.08),rgba(0,0,0,0.2))] p-5 shadow-inner shadow-black/40">
          {track.artUrl ? (
            <img src={track.artUrl} alt="" className="h-44 w-44 rounded-[1.1rem] border border-white/15 object-cover shadow-2xl shadow-black/50" />
          ) : (
            <div className="flex h-44 w-44 items-center justify-center rounded-[1.1rem] border border-white/15 bg-black/45 text-5xl font-black tracking-[0.18em] text-cyan-100">NP</div>
          )}
        </div>

        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="hud-chip">
              <span className="hud-led" />
              {streamLabel}
            </span>
            <span className="hud-badge">{track.player}</span>
            <span className="hud-badge">{track.playbackStatus || 'unknown'}</span>
          </div>

          <p className="hud-label mt-5">Now playing</p>
          <h1 className="mt-3 truncate text-[clamp(2rem,5vw,4.6rem)] font-black leading-none tracking-[-0.07em] text-white">{track.title}</h1>
          <p className="mt-3 truncate text-[clamp(1.1rem,2.3vw,2rem)] font-semibold text-cyan-100">{track.artist || 'Unknown artist'}</p>
          <p className="mt-2 truncate text-sm uppercase tracking-[0.24em] text-zinc-400">{track.album || 'Unknown album'}</p>

          <div className="mt-6">
            <div className="hud-progress hud-progress--large" style={{ '--hud-progress': `${percent}%` } as CSSProperties}>
              <div className="hud-progress__fill" />
            </div>
            <div className="mt-3 flex items-center justify-between text-xs uppercase tracking-[0.24em] text-zinc-400">
              <span>{formatDuration(track.positionMs)}</span>
              <span>{track.lengthMs > 0 ? formatDuration(track.lengthMs) : 'live'}</span>
            </div>
          </div>
        </div>
      </section>
    </main>
  );
}

function trackFromEvent(event: OverlayEvent): Track | null {
  if (event.type !== 'now_playing') {
    return null;
  }
  const data = isRecord(event.data) ? event.data : {};
  return {
    player: asString(data.player) || event.source || 'mpris',
    title: asString(data.title) || event.title || 'Now playing',
    artist: asString(data.artist) || event.message,
    album: asString(data.album),
    artUrl: asString(data.artUrl),
    playbackStatus: asString(data.playbackStatus) || 'Unknown',
    positionMs: asNumber(data.positionMs),
    lengthMs: asNumber(data.lengthMs),
  };
}

function formatDuration(ms: number) {
  if (!Number.isFinite(ms) || ms <= 0) {
    return '0:00';
  }
  const totalSeconds = Math.floor(ms / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${seconds.toString().padStart(2, '0')}`;
}

function asString(value: unknown) {
  return typeof value === 'string' ? value.trim() : '';
}

function asNumber(value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
