import type { ReactNode } from 'react';
import type { OverlayStreamStatus } from '../lib/eventStream';
import { Kicker, Mark, OverlayStage, RegMark } from './Brand';
import { SlabHeadline } from './SlabHeadline';
import type { Track } from '../routes/OverlayNowPlaying';

type PosterProps = {
  name: string;
  label: string;
  status: OverlayStreamStatus;
  tone?: 'purple' | 'ink';
  masthead: string;
  title: string;
  message?: string;
  side?: ReactNode;
  track?: Track | null;
  standby?: ReactNode;
};

// Full-frame 16:9 scene card in the brand's banner layout: green edge rule,
// mono masthead, condensed headline with the last word on a slab.
export function Poster({ name, label, status, tone = 'purple', masthead, title, message, side, track, standby }: PosterProps) {
  return (
    <OverlayStage name={name} status={status} label={label} className="sc-stage--poster">
      <section className={`sc-poster ${tone === 'ink' ? 'sc-poster--ink' : ''}`}>
        <header className="sc-poster__masthead">
          <Kicker>{masthead}</Kicker>
        </header>

        <div className="sc-poster__headline">
          <SlabHeadline as="h1" className="sc-poster__title" text={title} />
          {message ? <p className="sc-poster__message">{message}</p> : null}
          {standby}
        </div>

        <aside className="sc-poster__side">{side}</aside>

        <footer className="sc-poster__footer">
          {track ? (
            <p className="sc-label sc-poster__track">
              <span>
                {track.title}
                {track.artist ? ` / ${track.artist}` : ''}
              </span>
            </p>
          ) : (
            <span />
          )}
          <Mark tone={tone === 'ink' ? 'white' : 'ink'} size={112} />
        </footer>

        <RegMark className="sc-reg--block" />
      </section>
    </OverlayStage>
  );
}

export function PosterClock({ label, value }: { label: string; value: string }) {
  return (
    <div className={`sc-poster__clock ${value.length > 7 ? 'sc-poster__clock--long' : ''}`}>
      <p className="sc-label">{label}</p>
      <p className="sc-display">{value}</p>
    </div>
  );
}

export function PosterList({ items, label }: { items: string[]; label: string }) {
  if (items.length === 0) {
    return null;
  }
  return (
    <ol className="sc-poster__list" aria-label={label}>
      {items.map((item, index) => (
        <li key={`${item}-${index}`}>
          <span className="sc-label">{String(index + 1).padStart(2, '0')}</span>
          <span>{item}</span>
        </li>
      ))}
    </ol>
  );
}
