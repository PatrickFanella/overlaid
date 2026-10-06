import { EventHistoryPanel } from './components/EventHistoryPanel';
import { SessionLogPanel } from './components/SessionLogPanel';
import { StreamSessionPanel } from './components/StreamSessionPanel';
import { DashboardShell } from './DashboardShell';
import { useDeskStatus } from './deskStatus';
import { useRecentEvents } from './OnAir';
import { PageHead } from './ui';
import { useOperatorActions } from './useOperatorActions';

export function DashboardSessionsPage() {
  return (
    <DashboardShell activePath="/dashboard/sessions">
      <Sessions />
    </DashboardShell>
  );
}

function Sessions() {
  const status = useDeskStatus();
  const { sessionLog, clearLog } = useOperatorActions();
  const history = useRecentEvents(status.apiBaseUrl, status.checkedAt, 100, status.operatorSecret);

  return (
    <>
      <PageHead title="Sessions" lede="Open a session before a stream or rehearsal so its events are tagged, then export the report afterwards." />
      <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_420px]">
        <div className="grid gap-4">
          <StreamSessionPanel apiBaseUrl={status.apiBaseUrl} operatorSecret={status.operatorSecret} status={status.session} loading={status.loading} error={status.errors.session} onChange={status.setSession} />
          <EventHistoryPanel events={history.data?.events ?? []} loading={history.loading} error={history.error} limit={50} />
        </div>
        <SessionLogPanel entries={sessionLog} onClear={clearLog} />
      </div>
    </>
  );
}
