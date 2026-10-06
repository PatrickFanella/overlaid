import { useEffect, useState } from 'react';
import { defaultDashboardConfig, sanitizeDisplayConfig } from './dashboardConfig';
import { getOverlayEndpointUrl, type OverlayStreamStatus } from './eventStream';
import { isOverlayDemoMode } from './overlayParams';

export function useOverlayDisplay(status: OverlayStreamStatus) {
  const [display, setDisplay] = useState(() => ({ ...defaultDashboardConfig.display }));
  useEffect(() => {
    if (isOverlayDemoMode()) return;
    const controller = new AbortController();
    void fetch(getOverlayEndpointUrl('/api/overlay/display'), { signal: controller.signal })
      .then(async response => {
        if (!response.ok) throw new Error('Overlay settings unavailable');
        return response.json() as Promise<{ display?: unknown }>;
      }).then(result => {
        if (!controller.signal.aborted) setDisplay(sanitizeDisplayConfig(result.display));
      }).catch(() => {
        if (!controller.signal.aborted) setDisplay({ ...defaultDashboardConfig.display });
      });
    return () => controller.abort();
  }, [status]);
  return display;
}
