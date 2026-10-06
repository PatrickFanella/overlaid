import { useState } from 'react';
import { loadDashboardConfig } from '../lib/dashboardConfig';
import type { OverlayEvent } from '../lib/events';
import { useOverlayStream } from '../lib/useOverlayStream';
import { Kicker, OverlayStage, RegMark } from '../overlay/Brand';
import { asString, formatClock } from '../overlay/data';
import { SlabHeadline } from '../overlay/SlabHeadline';

export function OverlayAlertsPage() {
  const [display] = useState(() => loadDashboardConfig().display);
  const [alerts, setAlerts] = useState<OverlayEvent[]>([]);

  const status = useOverlayStream((event) => {
    if (event.type === 'clear') {
      setAlerts([]);
      return;
    }

    if (event.type !== 'alert' || event.data?.retainedStateKey) {
      return;
    }

    setAlerts((current) => {
      if (isDuplicateAlert(current, event, display.alertDedupeSeconds)) {
        return current;
      }
      return [event, ...current].slice(0, display.alertMaxVisible);
    });
  });

  return (
    <OverlayStage name="alerts" status={status} label="Alert feed">
      <section className="sc-alerts" role="log" aria-live="polite">
        {alerts.length === 0 ? (
          <div className="sc-standby">
            <p className="sc-standby__title">Waiting for alert events</p>
            <p className="sc-standby__hint">Alert feed · send a test alert from the dashboard</p>
          </div>
        ) : null}

        {alerts.map((alert) => (
          <article key={alert.id} className={`sc-alert sc-alert--${alert.severity}`}>
            <Kicker>
              {alertKind(alert)} <span className="sc-label--sep">{alert.source}</span> <span className="sc-label--sep">{formatClock(alert.createdAt)}</span>
            </Kicker>
            <SlabHeadline className="sc-alert__title" text={alert.title || 'Alert'} />
            {alert.message ? <p className="sc-alert__body">{alert.message}</p> : null}
            <RegMark className="sc-reg--block" />
          </article>
        ))}
      </section>
    </OverlayStage>
  );
}

function isDuplicateAlert(current: OverlayEvent[], next: OverlayEvent, dedupeSeconds: number) {
  if (dedupeSeconds <= 0) {
    return false;
  }

  const cutoff = Date.now() - dedupeSeconds * 1000;
  return current.some((alert) => alert.title === next.title && alert.message === next.message && Date.parse(alert.createdAt) >= cutoff);
}

function alertKind(alert: OverlayEvent) {
  const kind = asString(alert.data?.kind);
  if (kind) {
    return kind.replaceAll('_', ' ');
  }
  switch (alert.severity) {
    case 'success':
      return 'nice';
    case 'warning':
      return 'heads up';
    case 'error':
      return 'trouble';
    default:
      return 'alert';
  }
}
