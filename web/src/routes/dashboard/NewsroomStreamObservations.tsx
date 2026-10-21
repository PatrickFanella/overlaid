import { useEffect, useState, type ReactNode } from 'react';
import { demoStreamObservations, fetchStreamObservations, type StreamObservations } from '../../lib/streamObservations';
import { Empty, KeyValues, Notice, Panel, Tag } from './ui';

type Props = { apiBaseUrl: string; operatorSecret: string; scopeKey: string; demo: boolean };
type State = { kind: 'loading' } | { kind: 'error' } | { kind: 'ready'; report: StreamObservations };

// Remount before painting a different account/workspace/API scope. Cleanup and
// abort also fence responses from prior tabs, replaced reads and unmounts.
export function NewsroomStreamObservations(props: Props) {
  return <ObservationReader key={JSON.stringify([props.scopeKey, props.apiBaseUrl, props.demo])} {...props} />;
}
function ObservationReader({ apiBaseUrl, operatorSecret, demo }: Props) {
  const [generation, setGeneration] = useState(0);
  const [state, setState] = useState<State>(() => demo ? { kind: 'ready', report: demoStreamObservations } : { kind: 'loading' });
  useEffect(() => {
    if (demo) return;
    const controller = new AbortController();
    let current = true;
    const timeout = window.setTimeout(() => { controller.abort(); if (current) setState({ kind: 'error' }); }, 10_000);
    void fetchStreamObservations(apiBaseUrl, operatorSecret, controller.signal).then(report => {
      if (current && !controller.signal.aborted) setState({ kind: 'ready', report });
    }).catch(() => { if (current) setState({ kind: 'error' }); }).finally(() => window.clearTimeout(timeout));
    return () => { current = false; window.clearTimeout(timeout); controller.abort(); };
  }, [apiBaseUrl, operatorSecret, demo, generation]);
  function refresh() {
    setState({ kind: 'loading' });
    setGeneration(value => value + 1);
  }
  return <Panel title="Saved Twitch observations" description="Events retained for this workspace. Twitch may have changed since they were received." actions={<>
    {demo ? <Tag>Sample observations</Tag> : <button type="button" className="desk-btn desk-btn--small" disabled={state.kind === 'loading'} onClick={refresh}>Refresh observations</button>}
  </>}>
    {state.kind === 'loading' ? <p role="status">Loading saved Twitch observations…</p> : null}
    {state.kind === 'error' ? <Notice tone="warn" role="status">Saved Twitch observations are unavailable. Check this workspace’s connection and storage setup, then retry.</Notice> : null}
    {state.kind === 'ready' ? <StreamObservationSummary report={state.report} /> : null}
  </Panel>;
}
function At({ value }: { value: string }) {
  return <time dateTime={value} title={value}>{new Date(value).toLocaleString()}</time>;
}
export function StreamObservationSummary({ report }: { report: StreamObservations }) {
  return <div className="grid min-w-0 grid-cols-1 gap-4">
    {report.state === 'disconnected' ? <Empty>No active Twitch connection is saved for this workspace.</Empty> : report.state === 'empty' ? <Empty>No Twitch lifecycle or channel details have been retained for this connection.</Empty> : <div className="grid min-w-0 grid-cols-1 gap-5 md:grid-cols-2">
      <section className="min-w-0" aria-label="Last lifecycle event">
        <h3 className="desk-sub__title">Last lifecycle event</h3>
        {report.lifecycle ? <KeyValues items={[
          ['Event', report.lifecycle.kind === 'stream_online' ? 'Online event' : 'Offline event'],
          ['Occurred', <At value={report.lifecycle.occurredAt} />],
          ['Received', <At value={report.lifecycle.receivedAt} />],
          ...(report.lifecycle.kind === 'stream_online' ? [['Stream started', <At value={report.lifecycle.startedAt} />] as [string, ReactNode]] : []),
        ]} /> : <Empty>No lifecycle event has been retained.</Empty>}
      </section>
      <section className="min-w-0" aria-label="Last channel details">
        <h3 className="desk-sub__title">Last channel details</h3>
        {report.metadata ? <div className="grid min-w-0 grid-cols-1 gap-3">
          <p className="break-words font-medium [overflow-wrap:anywhere]">{report.metadata.title || 'The saved channel title was empty.'}</p>
          <KeyValues items={[
            ['Category', report.metadata.categoryName || (report.metadata.categoryId ? `Category ID ${report.metadata.categoryId}` : 'No category recorded')],
            ['Language', report.metadata.language || 'No language recorded'],
            ['Occurred', <At value={report.metadata.occurredAt} />],
            ['Received', <At value={report.metadata.receivedAt} />],
          ]} />
          {report.metadata.labels.length ? <p className="break-words [overflow-wrap:anywhere]">Content labels: {report.metadata.labels.join(', ')}</p> : null}
        </div> : <Empty>No channel details have been retained.</Empty>}
      </section>
    </div>}
    <p className="desk-field__hint">Saved records checked <At value={report.observedAt} />. Check the show list below for saved show boundaries.</p>
  </div>;
}
