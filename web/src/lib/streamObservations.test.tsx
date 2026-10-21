import { afterEach, describe, expect, it, vi } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { decodeStreamObservations, fetchStreamObservations, demoStreamObservations } from './streamObservations';
import { StreamObservationSummary } from '../routes/dashboard/NewsroomStreamObservations';

function report() {
  return {
    state: 'observed', channelId: 'channel-fixture', observedAt: '2026-10-21T21:05:00Z',
    lifecycle: { eventId: 'a'.repeat(64), kind: 'stream_online', streamId: 'stream', occurredAt: '2026-10-21T21:00:00Z', receivedAt: '2026-10-21T21:00:01Z', startedAt: '2026-10-21T20:59:00Z' },
    metadata: { eventId: 'b'.repeat(64), occurredAt: '2026-10-21T21:02:00Z', receivedAt: '2026-10-21T21:02:01Z', data: { title: '<img src=x onerror=alert(1)>', language: 'en', category_id: 'category', category_name: 'Just Chatting', content_classification_labels: ['MatureGame'] } },
  };
}
afterEach(() => vi.unstubAllGlobals());

describe('retained stream display projection', () => {
  it('selects only known metadata and escapes submitted text without live/delivery claims', () => {
    const raw = { ...report(), private_native_field: 'PRIVATE INPUT', tenant_id: 'PRIVATE TENANT', access_token: 'PRIVATE TOKEN' };
    const parsed = decodeStreamObservations(raw);
    expect(JSON.stringify(parsed)).not.toContain('PRIVATE');
    const html = renderToStaticMarkup(<StreamObservationSummary report={parsed} />);
    expect(html).toContain('&lt;img'); expect(html).not.toContain('<img');
    expect(html).toContain('Online event'); expect(html).toContain('Stream started');
    expect(html).toContain('2026-10-21T21:00:01Z'); expect(html).toContain('Just Chatting');
    expect(html).toContain('Check the show list below for saved show boundaries');
    expect(html).not.toContain('streamId'); expect(html).not.toContain('eventId');
    expect(html).not.toContain('Live now');
  });
  it('handles disconnected, empty and independently missing lifecycle/metadata', () => {
    for (const raw of [
      { state: 'disconnected', observedAt: report().observedAt, lifecycle: null, metadata: null },
      { state: 'empty', observedAt: report().observedAt, channelId: 'channel', lifecycle: null, metadata: null },
      { ...report(), lifecycle: null },
      { ...report(), metadata: null },
      { ...report(), lifecycle: { eventId: 'a'.repeat(64), kind: 'stream_offline', occurredAt: report().observedAt, receivedAt: report().observedAt } },
    ]) expect(() => renderToStaticMarkup(<StreamObservationSummary report={decodeStreamObservations(raw)} />)).not.toThrow();
    expect(renderToStaticMarkup(<StreamObservationSummary report={decodeStreamObservations({ state: 'disconnected', observedAt: report().observedAt, lifecycle: null, metadata: null })} />)).toContain('No active Twitch connection');
    expect(renderToStaticMarkup(<StreamObservationSummary report={{ ...demoStreamObservations, lifecycle: null }} />)).toContain('No lifecycle event');
  });
  it.each(['shape', 'state', 'disconnected-data', 'empty-data', 'missing-data', 'missing-channel', 'invalid-time', 'invalid-calendar', 'invalid-kind', 'missing-start', 'offline-start', 'duplicate-id', 'private-id', 'title-limit', 'null-labels', 'label-limit', 'empty-label'])('refuses malformed or contradictory %s reports', mode => {
    const raw: Record<string, unknown> = report();
    const lifecycle = raw.lifecycle as Record<string, unknown>;
    const data = (raw.metadata as { data: Record<string, unknown> }).data;
    switch (mode) {
      case 'shape': expect(() => decodeStreamObservations(null)).toThrow(); return;
      case 'state': raw.state = 'live'; break;
      case 'disconnected-data': raw.state = 'disconnected'; delete raw.channelId; break;
      case 'empty-data': raw.state = 'empty'; break;
      case 'missing-data': raw.lifecycle = null; raw.metadata = null; break;
      case 'missing-channel': delete raw.channelId; break;
      case 'invalid-time': raw.observedAt = 'now'; break;
      case 'invalid-calendar': raw.observedAt = '2026-02-30T00:00:00Z'; break;
      case 'invalid-kind': lifecycle.kind = 'channel_update'; break;
      case 'missing-start': delete lifecycle.startedAt; break;
      case 'offline-start': lifecycle.kind = 'stream_offline'; break;
      case 'duplicate-id': (raw.metadata as Record<string, unknown>).eventId = lifecycle.eventId; break;
      case 'private-id': lifecycle.eventId = 'PRIVATE'; break;
      case 'title-limit': data.title = 'x'.repeat(4097); break;
      case 'null-labels': data.content_classification_labels = null; break;
      case 'label-limit': data.content_classification_labels = Array(33).fill('label'); break;
      case 'empty-label': data.content_classification_labels = ['']; break;
    }
    expect(() => decodeStreamObservations(raw)).toThrow('Invalid saved Twitch observations');
  });
});

describe('retained stream read transport', () => {
  it('uses a cancellable authenticated GET and suppresses operator credentials for hosted workspaces', async () => {
    vi.stubGlobal('window', { sessionStorage: { getItem: () => JSON.stringify({ deploymentMode: 'hosted', tenantId: 'tenant-fixture', csrf: 'csrf-fixture' }) } });
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => report() });
    vi.stubGlobal('fetch', fetchMock);
    const controller = new AbortController();
    await fetchStreamObservations('http://example.test', 'operator-fixture', controller.signal);
    expect(fetchMock).toHaveBeenCalledWith('http://example.test/api/twitch/stream-observations', {
      method: 'GET', credentials: 'include', signal: controller.signal,
      headers: { Accept: 'application/json', 'X-Overlaid-Tenant': 'tenant-fixture', 'X-Overlaid-CSRF': 'csrf-fixture' },
    });
  });
  it('keeps the legacy operator read boundary and never exposes backend error body details', async () => {
    const text = vi.fn().mockResolvedValue('PRIVATE PROVIDER INPUT');
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 501, text });
    vi.stubGlobal('fetch', fetchMock);
    await expect(fetchStreamObservations('http://example.test', ' operator-fixture ', new AbortController().signal)).rejects.toThrow('Saved Twitch observations failed: 501');
    expect(text).not.toHaveBeenCalled();
    expect(fetchMock.mock.calls[0][1].headers).toEqual({ Accept: 'application/json', 'X-Overlay-Operator-Secret': 'operator-fixture' });
  });
});
