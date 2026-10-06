import { useState, type CSSProperties } from 'react';
import type { OverlayEvent } from '../lib/events';
import { useOverlayStream } from '../lib/useOverlayStream';
import { FisheyeMark, Kicker, OverlayStage } from '../overlay/Brand';
import { asNumber, asString, isRecord } from '../overlay/data';

export type Track = {
  player: string;
  title: string;
  artist: string;
  album: string;
  artUrl: string;
  playbackStatus: string;
  positionMs: number;
  lengthMs: number;
};

export function OverlayNowPlayingPage() {
  const [track, setTrack] = useState<Track | null>(null);

  const status = useOverlayStream((event) => {
    if (event.type === 'clear') {
      setTrack(null);
      return;
    }

    const next = trackFromEvent(event);
    if (next) {
      setTrack(next);
    }
  });

  if (!track) {
    return (
      <OverlayStage name="now-playing" status={status} label="Now playing">
        <div className="sc-standby">
          <p className="sc-standby__title">Waiting for MPRIS</p>
          <p className="sc-standby__hint">Now playing · start a local media player</p>
        </div>
      </OverlayStage>
    );
  }

  const paused = track.playbackStatus.toLowerCase() === 'paused';
  const percent = track.lengthMs > 0 ? Math.max(0, Math.min(100, Math.round((track.positionMs / track.lengthMs) * 100))) : 0;

  return (
    <OverlayStage name="now-playing" status={status} label="Now playing">
      <section key={`${track.title}-${track.artist}`} className={`sc-np ${paused ? 'sc-np--paused' : ''}`}>
        {track.artUrl ? <img className="sc-np__art" src={track.artUrl} alt="" /> : <FisheyeMark className="sc-np__art" size={136} />}
        <div className="sc-np__body">
          <Kicker>
            {paused ? 'paused' : 'now playing'} <span className="sc-label--sep">{track.player}</span>
          </Kicker>
          <h1 className="sc-display sc-np__title">{track.title}</h1>
          <p className="sc-np__artist">{[track.artist, track.album].filter(Boolean).join(' · ') || 'Unknown artist'}</p>
          {track.lengthMs > 0 ? (
            <div className="sc-np__progress">
              <span className="sc-label">{formatDuration(track.positionMs)}</span>
              <div className="sc-meter" style={{ '--meter': `${percent}%` } as CSSProperties}>
                <div className="sc-meter__fill" />
              </div>
              <span className="sc-label">{formatDuration(track.lengthMs)}</span>
            </div>
          ) : null}
        </div>
      </section>
    </OverlayStage>
  );
}

export function trackFromEvent(event: OverlayEvent): Track | null {
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
    positionMs: finiteOrZero(asNumber(data.positionMs)),
    lengthMs: finiteOrZero(asNumber(data.lengthMs)),
  };
}

function finiteOrZero(value: number) {
  return Number.isFinite(value) ? value : 0;
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
