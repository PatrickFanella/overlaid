import { useState } from 'react';
import type { OverlayEvent } from '../lib/events';
import { useOverlayStream } from '../lib/useOverlayStream';
import { asString, asStringArray } from '../overlay/data';
import { Poster, PosterClock, PosterList } from '../overlay/Poster';
import { trackFromEvent, type Track } from './OverlayNowPlaying';

type BRBState = {
  active: boolean;
  title: string;
  message: string;
  eta: string;
  items: string[];
};

// The scene is only on air when OBS switches to it, so the inactive state is
// still a finished card rather than an empty frame.
const defaultState: BRBState = {
  active: false,
  title: 'Be right back.',
  message: 'Short break. Chat stays open.',
  eta: '',
  items: ['Chat stays live', 'Music keeps playing', 'Back soon'],
};

export function OverlayBRBPage() {
  const [state, setState] = useState<BRBState>(defaultState);
  const [track, setTrack] = useState<Track | null>(null);

  const status = useOverlayStream((event) => {
    if (event.type === 'clear') {
      setState(defaultState);
      return;
    }
    if (event.type === 'brb') {
      setState(brbStateFromEvent(event));
      return;
    }
    const next = trackFromEvent(event);
    if (next) {
      setTrack(next);
    }
  });

  return (
    <Poster
      name="brb"
      label="BRB overlay"
      status={status}
      masthead="subcult.tv / intermission"
      title={state.title}
      message={state.message}
      track={track}
      standby={
        state.active ? null : (
          <p className="sc-standby">
            <span className="sc-standby__hint">Waiting for intermission command</span>
          </p>
        )
      }
      side={
        <>
          <PosterClock label="back in" value={state.eta || 'soon'} />
          <PosterList label="Intermission notes" items={state.items.length ? state.items : defaultState.items} />
        </>
      }
    />
  );
}

function brbStateFromEvent(event: OverlayEvent): BRBState {
  const data = event.data || {};
  const active = typeof data.active === 'boolean' ? data.active : true;
  if (!active) {
    return defaultState;
  }
  return {
    active,
    title: event.title || asString(data.title) || defaultState.title,
    message: event.message || asString(data.message) || defaultState.message,
    eta: asString(data.eta),
    items: asStringArray(data.items),
  };
}
