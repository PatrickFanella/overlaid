import { renderToString } from 'react-dom/server';
import { afterEach, describe, expect, it, vi } from 'vitest';
import App from './App';
import { getDefaultApiBaseUrl, getOverlayStreamUrl } from './lib/eventStream';
import { parseOverlayEvent } from './lib/events';
import { defaultDashboardConfig, loadDashboardConfig, type QuickAction } from './lib/dashboardConfig';
import { resolveQuickActionSequence, runQuickActionSequence } from './lib/quickActionSequences';
import { QuickActionRunner } from './routes/dashboard/components/QuickActionRunner';
import { ReadinessPanel } from './routes/dashboard/components/ReadinessPanel';

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

  it('renders the dashboard overview route', () => {
    const html = renderToString(<App pathname="/dashboard" />);

    expect(html).toContain('over-laid command center');
    expect(html).toContain('System status');
    expect(html).toContain('Twitch login');
  });

  it('renders the dashboard setup route', () => {
    const html = renderToString(<App pathname="/dashboard/setup" />);

    expect(html).toContain('OBS setup workflow');
    expect(html).toContain('Checklist');
    expect(html).toContain('Browser sources added');
  });

  it('renders the dashboard live preflight route', () => {
    const html = renderToString(<App pathname="/dashboard/live" />);

    expect(html).toContain('Live ops cockpit');
    expect(html).toContain('Backend readiness');
    expect(html).toContain('Readiness report unavailable');
    expect(html).toContain('Setup checklist summary');
    expect(html).toContain('Recent operator actions');
    expect(html).toContain('Live preflight');
    expect(html).toContain('Chat commands');
    expect(html).toContain('mods + broadcaster only');
  });

  it('renders the dashboard sources route', () => {
    const html = renderToString(<App pathname="/dashboard/sources" />);

    expect(html).toContain('Overlay source URLs');
    expect(html).toContain('/overlay/alerts');
    expect(html).toContain('Stream presets');
    expect(html).toContain('Custom CSS');
  });

  it('renders the dashboard control route', () => {
    const html = renderToString(<App pathname="/dashboard/control" />);

    expect(html).toContain('Stream control');
    expect(html).toContain('operator secret missing');
    expect(html).toContain('Edit secret');
    expect(html).toContain('Session log');
    expect(html).toContain('Recent operator actions');
    expect(html).toContain('Run Main preset');
    expect(html).toContain('Refresh Alerts');
    expect(html).toContain('require confirm');
    expect(html).toContain('Go Live Main');
    expect(html).toContain('Start BRB');
    expect(html).toContain('Sequences run listed actions in order');
    expect(html).toContain('Action IDs');
    expect(html).toContain('Stop sequence on first failed step');
  });

  it('renders the dashboard config route', () => {
    const html = renderToString(<App pathname="/dashboard/config" />);

    expect(html).toContain('Operator config');
    expect(html).toContain('Secret header');
    expect(html).toContain('MPRIS decision');
    expect(html).toContain('Overlay noise controls');
  });

  it('renders degraded readiness incidents', () => {
    const html = renderToString(
      <ReadinessPanel
        loading={false}
        error=""
        report={{ status: 'degraded', checks: [{ name: 'obs_auth', status: 'degraded', hint: 'Check OBS password.' }] }}
      />,
    );

    expect(html).toContain('Operator incidents');
    expect(html).toContain('obs auth');
    expect(html).toContain('Check OBS password.');
  });

  it('renders the goals overlay route', () => {
    const html = renderToString(<App pathname="/overlay/goals" />);

    expect(html).toContain('Goal rail');
    expect(html).toContain('Waiting for goal events');
  });

  it('renders the ticker overlay route', () => {
    const html = renderToString(<App pathname="/overlay/ticker" />);

    expect(html).toContain('Ticker rail');
    expect(html).toContain('Waiting for ticker events');
  });

  it('renders the now-playing overlay route', () => {
    const html = renderToString(<App pathname="/overlay/now-playing" />);

    expect(html).toContain('Now playing');
    expect(html).toContain('Waiting for MPRIS');
  });

  it('renders the brb overlay route', () => {
    const html = renderToString(<App pathname="/overlay/brb" />);

    expect(html).toContain('BRB overlay');
    expect(html).toContain('Waiting for intermission command');
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

  it('parses SSE brb payloads', () => {
    const event = parseOverlayEvent(
      '{"id":"brb","type":"brb","source":"dashboard","title":"Be right back","message":"Stretch break","data":{"active":true,"eta":"5 min","items":["Hydrate"]},"createdAt":"2026-06-10T00:00:00.000Z"}',
      'brb',
    );

    expect(event).toMatchObject({
      id: 'brb',
      type: 'brb',
      title: 'Be right back',
      message: 'Stretch break',
    });
    expect(event?.data).toMatchObject({ active: true, eta: '5 min' });
  });
});

describe('dashboard config loading', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('keeps persisted quick actions and appends missing defaults', () => {
    const persistedQuickActions = [
      {
        ...defaultDashboardConfig.quickActions[0],
        label: 'Go Main now',
      },
      {
        id: 'custom-hide',
        label: 'Custom Hide',
        kind: 'hideBRB' as const,
      },
      {
        ...defaultDashboardConfig.quickActions[6],
      },
    ];

    vi.stubGlobal('window', {
      localStorage: {
        getItem: () => JSON.stringify({ quickActions: persistedQuickActions }),
      },
    });

    const config = loadDashboardConfig();

    expect(config.quickActions.map((action) => action.id)).toEqual([
      'main-scene',
      'custom-hide',
      'hide-brb',
      'preset-main',
      'refresh-alerts',
      'brb-scene',
      'sample-alert',
      'show-brb',
      'sequence-go-live-main',
      'sequence-start-brb',
    ]);
    expect(config.quickActions[0]).toMatchObject({ label: 'Go Main now' });
    expect(config.quickActions[1]).toMatchObject({ label: 'Custom Hide' });
  });
});

describe('quick action sequence utilities', () => {
  it('hides no-op sequence buttons while keeping runnable actions', () => {
    const actions: QuickAction[] = [
      { id: 'a', label: 'Run Main', kind: 'setScene', sceneName: 'Main' },
      { id: 'empty', label: 'Empty Sequence', kind: 'sequence', actionIds: ['missing', 'nested'], stopOnError: true },
      { id: 'nested', label: 'Nested', kind: 'sequence', actionIds: ['a'], stopOnError: true },
      { id: 'live', label: 'Go Live', kind: 'sequence', actionIds: ['a'], stopOnError: true },
    ];

    const html = renderToString(
      <QuickActionRunner
        actions={actions}
        apiBaseUrl="http://localhost:8080"
        operatorSecret="secret"
        isRunning={false}
        runAction={() => {
          throw new Error('unused');
        }}
      />,
    );

    expect(html).toContain('Run Main');
    expect(html).toContain('Go Live');
    expect(html).not.toContain('Empty Sequence');
  });

  it('resolves atomic quick actions in sequence order', () => {
    const actions = [
      { id: 'a', label: 'Go Main', kind: 'setScene' as const, sceneName: 'Main' },
      { id: 'b', label: 'Refresh Alerts', kind: 'refreshSource' as const, inputName: 'Alerts Browser' },
      { id: 'seq', label: 'Go Live', kind: 'sequence' as const, actionIds: ['a', 'b'], stopOnError: true },
    ];

    const resolved = resolveQuickActionSequence(actions[2], actions);

    expect(resolved.steps.map((action) => action.id)).toEqual(['a', 'b']);
    expect(resolved.warnings).toEqual([]);
  });

  it('skips missing and nested sequence actions with warnings', () => {
    const actions = [
      { id: 'a', label: 'Go Main', kind: 'setScene' as const, sceneName: 'Main' },
      { id: 'nested', label: 'Nested', kind: 'sequence' as const, actionIds: ['a'], stopOnError: true },
      { id: 'seq', label: 'Go Live', kind: 'sequence' as const, actionIds: ['a', 'missing', 'nested', 'seq'], stopOnError: true },
    ];

    const resolved = resolveQuickActionSequence(actions[2], actions);

    expect(resolved.steps.map((action) => action.id)).toEqual(['a']);
    expect(resolved.warnings).toContain('Missing action: missing');
    expect(resolved.warnings).toContain('Nested sequence skipped: nested');
    expect(resolved.warnings).toContain('Nested sequence skipped: seq');
  });

  it('runs sequence child actions in order', async () => {
    const actions = [
      { id: 'a', label: 'Go Main', kind: 'setScene' as const, sceneName: 'Main' },
      { id: 'b', label: 'Refresh Alerts', kind: 'refreshSource' as const, inputName: 'Alerts Browser' },
      { id: 'seq', label: 'Go Live', kind: 'sequence' as const, actionIds: ['a', 'b'], stopOnError: true },
    ];
    const calls: string[] = [];
    const sequence = actions[2];
    if (sequence.kind !== 'sequence') throw new Error('expected sequence');

    const result = await runQuickActionSequence(sequence, actions, async (action) => {
      calls.push(action.id);
    });

    expect(calls).toEqual(['a', 'b']);
    expect(result).toEqual({ warnings: [], failures: [], ran: 2 });
  });

  it('stops after the first failed step when stopOnError is true', async () => {
    const actions = [
      { id: 'a', label: 'Go Main', kind: 'setScene' as const, sceneName: 'Main' },
      { id: 'b', label: 'Refresh Alerts', kind: 'refreshSource' as const, inputName: 'Alerts Browser' },
      { id: 'seq', label: 'Go Live', kind: 'sequence' as const, actionIds: ['a', 'b'], stopOnError: true },
    ];
    const calls: string[] = [];
    const sequence = actions[2];
    if (sequence.kind !== 'sequence') throw new Error('expected sequence');

    const result = await runQuickActionSequence(sequence, actions, async (action) => {
      calls.push(action.id);
      throw new Error('boom');
    });

    expect(calls).toEqual(['a']);
    expect(result.failures).toEqual(['Go Main: boom']);
  });

  it('continues after failed steps when stopOnError is false', async () => {
    const actions = [
      { id: 'a', label: 'Go Main', kind: 'setScene' as const, sceneName: 'Main' },
      { id: 'b', label: 'Refresh Alerts', kind: 'refreshSource' as const, inputName: 'Alerts Browser' },
      { id: 'seq', label: 'Go Live', kind: 'sequence' as const, actionIds: ['a', 'b'], stopOnError: false },
    ];
    const calls: string[] = [];
    const sequence = actions[2];
    if (sequence.kind !== 'sequence') throw new Error('expected sequence');

    const result = await runQuickActionSequence(sequence, actions, async (action) => {
      calls.push(action.id);
      throw new Error('boom');
    });

    expect(calls).toEqual(['a', 'b']);
    expect(result.failures).toEqual(['Go Main: boom', 'Refresh Alerts: boom']);
  });
});
