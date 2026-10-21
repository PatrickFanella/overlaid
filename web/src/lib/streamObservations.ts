import { requestJSON } from './apiRequest';
import { loadAccountContext } from './accountContext';

export type ObservationTimes = { eventId: string; occurredAt: string; receivedAt: string };
export type LifecycleObservation = ObservationTimes & (
  { kind: 'stream_online'; startedAt: string; streamId: string } |
  { kind: 'stream_offline'; streamId?: string }
);
export type MetadataObservation = ObservationTimes & {
  title: string; language: string; categoryId: string; categoryName: string; labels: string[];
};
export type StreamObservations = {
  state: 'disconnected' | 'empty' | 'observed'; observedAt: string; channelId?: string;
  lifecycle: LifecycleObservation | null; metadata: MetadataObservation | null;
};

function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid saved Twitch observations');
  return value as Record<string, unknown>;
}
function text(value: unknown, max: number): string {
  if (typeof value !== 'string' || new TextEncoder().encode(value).length > max) throw new Error('Invalid saved Twitch observations');
  return value;
}
function timestamp(value: unknown): string {
  const at = text(value, 64);
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(at) || !Number.isFinite(Date.parse(at)) || Date.parse(at) <= 0 || new Date(`${at.slice(0, 10)}T00:00:00Z`).toISOString().slice(0, 10) !== at.slice(0, 10)) throw new Error('Invalid saved Twitch observations');
  return at;
}
function times(value: Record<string, unknown>): ObservationTimes {
  const eventId = text(value.eventId, 64);
  if (!/^[a-f0-9]{64}$/.test(eventId)) throw new Error('Invalid saved Twitch observations');
  return { eventId, occurredAt: timestamp(value.occurredAt), receivedAt: timestamp(value.receivedAt) };
}

// Construct only the fixed display projection. Extra native/private fields never
// become component props, and contradictory success reports fail closed.
export function decodeStreamObservations(input: unknown): StreamObservations {
  const value = record(input);
  const observedAt = timestamp(value.observedAt);
  if (!['disconnected', 'empty', 'observed'].includes(value.state as string)) throw new Error('Invalid saved Twitch observations');
  const channelId = value.channelId === undefined ? undefined : text(value.channelId, 256);
  if (value.state === 'disconnected' ? Boolean(channelId) : !channelId) throw new Error('Invalid saved Twitch observations');
  let lifecycle: LifecycleObservation | null = null;
  if (value.lifecycle !== null) {
    const row = record(value.lifecycle);
    const base = times(row);
    if (row.kind === 'stream_online') {
      const streamId = text(row.streamId, 1024);
      if (!streamId.trim()) throw new Error('Invalid saved Twitch observations');
      lifecycle = { ...base, kind: 'stream_online', streamId, startedAt: timestamp(row.startedAt) };
    } else if (row.kind === 'stream_offline' && row.startedAt === undefined) {
      lifecycle = { ...base, kind: 'stream_offline', ...(row.streamId !== undefined ? { streamId: text(row.streamId, 1024) } : {}) };
    } else throw new Error('Invalid saved Twitch observations');
  }
  let metadata: MetadataObservation | null = null;
  if (value.metadata !== null) {
    const row = record(value.metadata), data = record(row.data);
    if (!Array.isArray(data.content_classification_labels) || data.content_classification_labels.length > 32) throw new Error('Invalid saved Twitch observations');
    const labels = data.content_classification_labels.map(label => {
      const result = text(label, 128);
      if (!result.trim()) throw new Error('Invalid saved Twitch observations');
      return result;
    });
    metadata = { ...times(row), title: text(data.title, 4096), language: text(data.language, 128), categoryId: text(data.category_id, 1024), categoryName: text(data.category_name, 1024), labels };
  }
  const hasInput = Boolean(lifecycle || metadata);
  if ((value.state === 'observed') !== hasInput || lifecycle && metadata && lifecycle.eventId === metadata.eventId) throw new Error('Invalid saved Twitch observations');
  return { state: value.state as StreamObservations['state'], observedAt, ...(channelId ? { channelId } : {}), lifecycle, metadata };
}

export async function fetchStreamObservations(baseUrl: string, operatorSecret: string, signal: AbortSignal): Promise<StreamObservations> {
  const value = await requestJSON<unknown>('/api/twitch/stream-observations', {
    method: 'GET', errorLabel: 'Saved Twitch observations', includeDetail: false,
    explicitBaseUrl: baseUrl, operatorSecret: loadAccountContext().deploymentMode === 'hosted' ? undefined : operatorSecret, signal,
  });
  return decodeStreamObservations(value);
}

export const demoStreamObservations: StreamObservations = {
  state: 'observed', channelId: 'sample', observedAt: '2026-10-21T21:05:00Z',
  lifecycle: { eventId: 'a'.repeat(64), kind: 'stream_online', streamId: 'sample-stream', occurredAt: '2026-10-21T21:00:00Z', receivedAt: '2026-10-21T21:00:01Z', startedAt: '2026-10-21T20:59:00Z' },
  metadata: { eventId: 'b'.repeat(64), occurredAt: '2026-10-21T21:02:00Z', receivedAt: '2026-10-21T21:02:01Z', title: 'Evening desk · community stories and source checks', categoryName: 'Just Chatting', categoryId: 'sample-category', language: 'en', labels: [] },
};
