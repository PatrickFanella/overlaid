import { useEffect, useRef, useState } from 'react';
import { connectOverlayStream, getOverlayApiBaseUrl, type OverlayStreamStatus } from './eventStream';
import type { OverlayEvent } from './events';
import { buildDemoChatEvent, demoEvents, stampDemoEvent } from './overlayDemo';
import { isOverlayDemoMode } from './overlayParams';

// One SSE subscription per overlay. In demo mode the overlay receives sample
// events on a timer and never opens a connection.
export function useOverlayStream(onEvent: (event: OverlayEvent) => void) {
  const [status, setStatus] = useState<OverlayStreamStatus>('connecting');
  const handler = useRef(onEvent);
  handler.current = onEvent;

  useEffect(() => {
    if (isOverlayDemoMode()) {
      setStatus('live');
      return playDemoEvents((event) => handler.current(event));
    }

    const stream = connectOverlayStream(getOverlayApiBaseUrl(), {
      onEvent: (event) => handler.current(event),
      onStatusChange: setStatus,
    });

    return () => stream.close();
  }, []);

  return status;
}

function playDemoEvents(emit: (event: OverlayEvent) => void) {
  let sequence = 0;
  const timers: ReturnType<typeof setTimeout>[] = [];

  demoEvents.forEach((event, index) => {
    timers.push(setTimeout(() => emit(stampDemoEvent(event, sequence++)), 150 + index * 350));
  });

  for (let index = 0; index < 5; index += 1) {
    timers.push(setTimeout(() => emit(stampDemoEvent(buildDemoChatEvent(index), sequence++)), 100 + index * 250));
  }

  let chatIndex = 5;
  const chatTimer = setInterval(() => emit(stampDemoEvent(buildDemoChatEvent(chatIndex++), sequence++)), 3200);
  const alertTimer = setInterval(() => emit(stampDemoEvent(demoEvents[sequence % 4], sequence++)), 7000);

  return () => {
    timers.forEach(clearTimeout);
    clearInterval(chatTimer);
    clearInterval(alertTimer);
  };
}
