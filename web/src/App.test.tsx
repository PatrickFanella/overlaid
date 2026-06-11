import { renderToString } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import App from './App';
import { getDefaultApiBaseUrl, getOverlayStreamUrl } from './lib/eventStream';
import { parseOverlayEvent } from './lib/events';

describe('App routing', () => {
  it('renders the home page with overlay routes', () => {
    const html = renderToString(<App pathname="/" />);

    expect(html).toContain('Overlay routes');
    expect(html).toContain('/overlay/alerts');
    expect(html).toContain('Twitch login');
    expect(html).toContain('patrick__eff');
  });

  it('renders the alerts overlay route', () => {
    const html = renderToString(<App pathname="/overlay/alerts" />);

    expect(html).toContain('Alert feed');
    expect(html).toContain('Waiting for alert events');
  });

  it('renders the dashboard route', () => {
    const html = renderToString(<App pathname="/dashboard" />);

    expect(html).toContain('over-laid command center');
    expect(html).toContain('Send test alert');
    expect(html).toContain('Scene controls');
  });

  it('renders the goals overlay route', () => {
    const html = renderToString(<App pathname="/overlay/goals" />);

    expect(html).toContain('Goal rail');
    expect(html).toContain('Waiting for goal events');
  });

  it('renders the ticker overlay route', () => {
    const html = renderToString(<App pathname="/overlay/ticker" />);

    expect(html).toContain('Ticker rail');
    expect(html).toContain('Stream systems nominal');
  });

  it('renders the now-playing overlay route', () => {
    const html = renderToString(<App pathname="/overlay/now-playing" />);

    expect(html).toContain('Now playing');
    expect(html).toContain('Waiting for MPRIS');
  });

  it('renders remaining placeholder overlay routes', () => {
    const html = renderToString(<App pathname="/overlay/brb" />);

    expect(html).toContain('BRB overlay');
    expect(html).toContain('OBS browser-source ready');
  });

  it('renders the chat overlay route', () => {
    const html = renderToString(<App pathname="/overlay/chat" />);

    expect(html).toContain('Waiting for Twitch chat');
  });

  it('renders the debug overlay route', () => {
    const html = renderToString(<App pathname="/overlay/debug" />);

    expect(html).toContain('Event inspector');
  });
});

describe('overlay event utilities', () => {
  it('builds the SSE URL from the configured API base', () => {
    expect(getOverlayStreamUrl('http://localhost:8080')).toBe('http://localhost:8080/api/events/stream');
    expect(getOverlayStreamUrl('https://example.com/api/')).toBe('https://example.com/api/events/stream');
  });

  it('derives the default API URL from the browser host for homelab builds', () => {
    expect(getDefaultApiBaseUrl('http://over-laid.local:5173')).toBe('http://over-laid.local:8080');
    expect(getDefaultApiBaseUrl('https://overlay.example.test')).toBe('https://overlay.example.test:8080');
  });

  it('parses SSE alert payloads', () => {
    const event = parseOverlayEvent(
      '{"id":"abc","type":"alert","source":"manual","title":"Hello","message":"World","severity":"success","createdAt":"2026-06-10T00:00:00.000Z"}',
      'alert',
    );

    expect(event).toMatchObject({
      id: 'abc',
      type: 'alert',
      source: 'manual',
      title: 'Hello',
      message: 'World',
      severity: 'success',
    });
  });

  it('parses SSE now-playing payloads', () => {
    const event = parseOverlayEvent(
      '{"id":"np","type":"now_playing","source":"mpris","title":"Song","message":"Artist","data":{"player":"spotify","positionMs":30000,"lengthMs":120000},"createdAt":"2026-06-10T00:00:00.000Z"}',
      'now_playing',
    );

    expect(event).toMatchObject({
      id: 'np',
      type: 'now_playing',
      source: 'mpris',
      title: 'Song',
    });
    expect(event?.data).toMatchObject({ player: 'spotify', positionMs: 30000 });
  });
});
