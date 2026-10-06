import { useEffect, useState, type CSSProperties } from 'react';
import { getOverlayNumberParam, getOverlayParam } from '../lib/overlayParams';
import { useOverlayStream } from '../lib/useOverlayStream';
import { Kicker, Mark, OverlayStage, RegMark } from '../overlay/Brand';
import { Poster, PosterClock, PosterList } from '../overlay/Poster';
import { trackFromEvent, type Track } from './OverlayNowPlaying';

// Scene cards configured per OBS browser source through query parameters, so
// each scene collection can carry its own copy without a dashboard round trip.

function useLatestTrack() {
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
  return { status, track };
}

function splitList(value: string) {
  return value
    .split('|')
    .map((item) => item.trim())
    .filter(Boolean)
    .slice(0, 4);
}

/**
 * /overlay/starting
 *   ?minutes=5          countdown from page load
 *   ?at=20:00           countdown to a local wall-clock time (wins over minutes)
 *   ?title=, ?message=  headline and supporting line
 *   ?items=a|b|c        up to four agenda lines
 */
export function OverlayStartingPage() {
  const { status, track } = useLatestTrack();
  const [deadline] = useState(() => resolveDeadline(getOverlayParam('at'), getOverlayNumberParam('minutes', 5)));
  const remaining = useCountdown(deadline);
  const items = splitList(getOverlayParam('items'));

  return (
    <Poster
      name="starting"
      label="Starting soon"
      status={status}
      masthead="subcult.tv / live shortly"
      title={getOverlayParam('title') || 'Starting soon.'}
      message={getOverlayParam('message') || 'Grab a drink. We go live in a minute.'}
      track={track}
      side={
        <>
          <PosterClock label={remaining > 0 ? 'live in' : 'live'} value={remaining > 0 ? formatCountdown(remaining) : 'Now'} />
          <PosterList label="On the agenda" items={items} />
        </>
      }
    />
  );
}

/**
 * /overlay/ending
 *   ?title=, ?message=  headline and supporting line
 *   ?next=              next stream line, shown in the side block
 *   ?link=              destination, defaults to subcult.tv
 */
export function OverlayEndingPage() {
  const { status, track } = useLatestTrack();
  const next = getOverlayParam('next');

  return (
    <Poster
      name="ending"
      label="Stream ending"
      status={status}
      tone="ink"
      masthead="subcult.tv / end of transmission"
      title={getOverlayParam('title') || 'Thanks for watching.'}
      message={getOverlayParam('message') || 'Clips and the archive go up after the stream.'}
      track={track}
      side={
        <>
          {next ? <PosterClock label="next stream" value={next} /> : null}
          <PosterList label="Where to find us" items={[getOverlayParam('link') || 'subcult.tv']} />
        </>
      }
    />
  );
}

/**
 * /overlay/camera
 *   ?name=   nameplate text, defaults to the configured channel
 *   ?role=   second nameplate line; omit to hide
 *   ?frame=  frame thickness in px (default 14)
 *   ?mark=0  hide the corner mark
 * Size the browser source to the camera source and stack it on top.
 */
export function OverlayCameraPage() {
  const name = getOverlayParam('name') || 'patrick__eff';
  const role = getOverlayParam('role');
  const frame = getOverlayNumberParam('frame', 14);
  const showMark = getOverlayParam('mark') !== '0';

  return (
    <OverlayStage name="camera" status="live" label="Camera frame">
      <div className="sc-camera" style={{ '--frame': `${Math.max(0, frame)}px` } as CSSProperties}>
        <RegMark className="sc-camera__corner--tl" />
        <RegMark className="sc-camera__corner--tr" />
        <RegMark className="sc-camera__corner--bl" />
        <RegMark className="sc-camera__corner--br" />
      </div>
      {showMark ? <Mark className="sc-camera__mark" size={72} /> : null}
      <div className="sc-nameplate">
        <p className="sc-display sc-nameplate__name">{name}</p>
        {role ? <Kicker className="sc-nameplate__role">{role}</Kicker> : null}
      </div>
    </OverlayStage>
  );
}

function resolveDeadline(at: string, minutes: number) {
  const match = /^(\d{1,2}):(\d{2})$/.exec(at);
  if (match) {
    const target = new Date();
    target.setHours(Number(match[1]), Number(match[2]), 0, 0);
    if (target.getTime() < Date.now() - 60_000) {
      target.setDate(target.getDate() + 1);
    }
    return target.getTime();
  }
  return Date.now() + Math.max(0, minutes) * 60_000;
}

function useCountdown(deadline: number) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  return Math.max(0, deadline - now);
}

function formatCountdown(ms: number) {
  const total = Math.ceil(ms / 1000);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  const mmss = `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
  return hours > 0 ? `${hours}:${mmss}` : mmss;
}
