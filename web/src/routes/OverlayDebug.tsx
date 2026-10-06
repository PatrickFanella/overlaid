import { useState } from 'react';
import { getOverlayApiBaseUrl } from '../lib/eventStream';
import type { OverlayEvent } from '../lib/events';
import { useOverlayStream } from '../lib/useOverlayStream';
import { Kicker, OverlayStage } from '../overlay/Brand';

// Setup tool, not an on-air asset: the connection pip always shows here.
export function OverlayDebugPage() {
  const [events, setEvents] = useState<OverlayEvent[]>([]);

  const status = useOverlayStream((event) => {
    setEvents((current) => [event, ...current].slice(0, 8));
  });

  return (
    <OverlayStage name="debug" status={status} label="Event inspector" className="sc-stage--setup">
      <section className="sc-debug">
        <Kicker>/overlay/debug · {status}</Kicker>
        <h1 className="sc-display">Event inspector</h1>
        <p className="sc-label">tail -f {getOverlayApiBaseUrl()}/api/events/stream</p>

        {events.length === 0 ? <p className="sc-standby__hint">Waiting for events…</p> : null}

        {events.map((event) => (
          <pre key={event.id}>{JSON.stringify(event, null, 2)}</pre>
        ))}
      </section>
    </OverlayStage>
  );
}
