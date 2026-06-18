import { renderToString } from 'react-dom/server';
import { afterEach, describe, expect, it, vi } from 'vitest';
import App from './App';
import { dashboardRoutes } from './lib/dashboardRoutes';
import { getDefaultApiBaseUrl, getOverlayStreamUrl } from './lib/eventStream';
import { deleteTwitchReward, fetchBotStatus, fetchTwitchInteractions, saveBotCommand, type StreamSessionReport } from './lib/api';
import { parseOverlayEvent } from './lib/events';
import {
  defaultDashboardConfig,
  loadDashboardConfig,
  loadOperatorSecret,
  normalizeDashboardConfig,
  saveDashboardConfig,
  saveOperatorSecret,
  type QuickAction,
} from './lib/dashboardConfig';
import { formatQuickActionRunResult, getConfirmableActionLabel, getVisibleQuickActions, nextConfirmableActionState } from './lib/controlActions';
import { buildHideBRBPayload, buildShowBRBPayload, parseBRBItems } from './lib/brbPayloads';
import { findMatchingChannelPointRule, normalizeImportedTwitchInteractionRules, updateTwitchAliasRule, updateTwitchRewardRule } from './lib/twitchInteractionRules';
import {
  exportDashboardConfig,
  getActiveAutomationProfile,
  importDashboardConfig,
  selectQuickActionsForProfile,
} from './lib/automationProfiles';
import { resolveQuickActionSequence, runQuickActionSequence } from './lib/quickActionSequences';
import { overlayRoutes } from './lib/overlayRoutes';
import { BuildInfoPanel } from './routes/dashboard/components/BuildInfoPanel';
import { QuickActionRunner } from './routes/dashboard/components/QuickActionRunner';
import { EventHistoryPanel } from './routes/dashboard/components/EventHistoryPanel';
import { ReadinessPanel } from './routes/dashboard/components/ReadinessPanel';
import { TwitchInteractionsManager } from './routes/dashboard/components/TwitchInteractionsManager';
import { RecoveryConsolePanel } from './routes/dashboard/components/RecoveryConsolePanel';
import { SessionLogPanel } from './routes/dashboard/components/SessionLogPanel';
import { validateQuickActionsAgainstOBS } from './lib/obsValidation';
import { formatSessionReportMarkdown, sessionReportFilename } from './lib/sessionReports';

describe('App routing', () => {
  it('renders the home page with overlay routes', () => {
    const html = renderToString(<App pathname="/" />);

    expect(html).toContain('Overlay routes');
    expect(html).toContain('/overlay/alerts');
    expect(html).toContain('Twitch login');
    expect(html).toContain('patrick__eff');
  });

  it.each(dashboardRoutes)('renders declared dashboard route $path', (route) => {
    const html = renderToString(<App pathname={route.path} />);

    expect(html).toContain('Broadcast ops console');
    expect(html).toContain(route.navLabel);
    expect(html).toContain(route.landmarkText);
  });

  it('renders the broadcast ops console shell hierarchy', () => {
    const html = renderToString(<App pathname="/dashboard" />);

    expect(html).toContain('Broadcast ops console');
    expect(html).toContain('Monitor');
    expect(html).toContain('Operate');
    expect(html).toContain('Configure');
  });

  it.each(overlayRoutes)('renders declared overlay route $path', (route) => {
    const html = renderToString(<App pathname={route.path} />);

    expect(html).toContain(route.title);
  });

  it('renders home fallback for unknown paths', () => {
    const html = renderToString(<App pathname="/not-a-real-route" />);

    expect(html).toContain('Overlay routes');
    expect(html).toContain('/not-a-real-route');
    expect(html).toContain('pick a route below');
  });

  it('renders the alerts overlay route', () => {
    const html = renderToString(<App pathname="/overlay/alerts" />);

    expect(html).toContain('Alert feed');
    expect(html).toContain('Waiting for alert events');
  });

  it('renders the dashboard overview route', () => {
    const html = renderToString(<App pathname="/dashboard" />);

    expect(html).toContain('Broadcast ops console');
    expect(html).toContain('Mission control');
    expect(html).toContain('Monitor first');
    expect(html).toContain('System status');
    expect(html).toContain('Twitch login');
    expect(html).toContain('Build info');
    expect(html).toContain('/version');
    expect(html).toContain('Build report unavailable.');
    expect(html).toContain('Ops route guide');
    expect(html).toContain('monitor / recover');
    expect(html).toContain('edit rules / profiles');
    expect(html).toContain('package / backup');
    expect(html).toContain(
      'https://git.subcult.tv/PatrickFanella/over-laid/src/branch/main/docs/runbooks/production-packaging.md',
    );
  });

  it('renders the dashboard setup route', () => {
    const html = renderToString(<App pathname="/dashboard/setup" />);

    expect(html).toContain('OBS setup workflow');
    expect(html).toContain('Checklist');
    expect(html).toContain('Browser sources added');
  });

  it('renders the dashboard live preflight route', () => {
    const html = renderToString(<App pathname="/dashboard/live" />);

    expect(html).toContain('Live command');
    expect(html).toContain('Live ops cockpit');
    expect(html).toContain('Check readiness, recover production issues');
    expect(html).toContain('Recovery console');
    expect(html).toContain('no destructive browser actions');
    expect(html).toContain('Production recovery');
    expect(html).toContain('Packaging + backups');
    expect(html).toContain('Build info');
    expect(html).toContain('Build report unavailable.');
    expect(html).toContain('Backend readiness');
    expect(html).toContain('Readiness report unavailable');
    expect(html).toContain('Setup checklist summary');
    expect(html).toContain('Recent operator actions');
    expect(html).toContain('Event history');
    expect(html).toContain('No persisted events yet.');
    expect(html).toContain('Live preflight');
    expect(html).toContain('Chat commands');
    expect(html).toContain('mods + broadcaster only');
  });

  it('renders the recovery console on the server without browser APIs', () => {
    const html = renderToString(<RecoveryConsolePanel />);

    expect(html).toContain('Recovery console');
    expect(html).toContain('no destructive browser actions');
    expect(html).toContain('Production recovery');
    expect(html).toContain(
      'https://git.subcult.tv/PatrickFanella/over-laid/src/branch/main/docs/runbooks/production-recovery.md',
    );
  });

  it('renders the dashboard sources route', () => {
    const html = renderToString(<App pathname="/dashboard/sources" />);

    expect(html).toContain('Overlay source URLs');
    expect(html).toContain('/overlay/alerts');
    expect(html).toContain('Active profile');
    expect(html).toContain('Full console');
    expect(html).toContain('Stream presets');
    expect(html).toContain('Custom CSS');
  });

  it('renders the dashboard control route', () => {
    const html = renderToString(<App pathname="/dashboard/control" />);

    expect(html).toContain('Stream control');
    expect(html).toContain('operator secret missing');
    expect(html).toContain('Edit secret');
    expect(html).toContain('Active profile');
    expect(html).toContain('Full console');
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

    expect(html).toContain('Configure deliberately');
    expect(html).toContain('Operator config');
    expect(html).toContain('Secrets stay in browser session state');
    expect(html).toContain('Secret header');
    expect(html).toContain('Automation profiles');
    expect(html).toContain('Twitch interaction rules');
    expect(html).toContain('Active profile');
    expect(html).toContain('Export dashboard config');
    expect(html).toContain('Import dashboard config');
    expect(html).toContain('operator secret is not exported');
    expect(html).toContain('MPRIS decision');
    expect(html).toContain('Overlay noise controls');
  });

  it('renders bot operations safety guidance', () => {
    const html = renderToString(<App pathname="/dashboard/bot" />);

    expect(html).toContain('Operate with guardrails');
    expect(html).toContain('Bot operations');
    expect(html).toContain('guarded destructive actions');
    expect(html).toContain('Nuke dry-run planner');
    expect(html).toContain('I reviewed this dry-run plan');
    expect(html).toContain('Radiation minutes');
  });

  it('renders the twitch interactions manager shell on the server without fetching', () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const html = renderToString(<TwitchInteractionsManager apiBaseUrl="http://example.test" operatorSecret="" />);

    expect(html).toContain('Twitch interaction rules');
    expect(html).toContain('Chat aliases');
    expect(html).toContain('Channel point rewards');
    expect(html).toContain('Operator secret required to save');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('renders reward queue safety guidance', () => {
    const html = renderToString(<TwitchInteractionsManager apiBaseUrl="http://example.test" operatorSecret="secret" />);

    expect(html).toContain('Reward queue');
    expect(html).toContain('Load manageable rewards, create simple rewards');
    expect(html).toContain('Delete requires confirmation');
    expect(html).toContain('New reward title');
    expect(html).toContain('Global cooldown seconds');
    expect(html).toContain('Max per stream');
    expect(html).toContain('Max per user/stream');
    expect(html).toContain('zero to disable optional limits');
  });

  it('renders the build info panel on the server without fetching', () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const html = renderToString(<BuildInfoPanel apiBaseUrl="http://example.test" />);

    expect(html).toContain('Build info');
    expect(html).toContain('/version');
    expect(html).toContain('Build report unavailable.');
    expect(fetchMock).not.toHaveBeenCalled();
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

  it('renders the event history panel with optional fields', () => {
    const html = renderToString(
      <EventHistoryPanel
        loading={false}
        error=""
        events={[
          {
            id: 'evt-2',
            type: 'custom_command',
            source: 'twitch-chat',
            title: 'Mods called a command',
            message: 'Command /go_live was executed',
            severity: 'info',
            createdAt: '2026-06-13T12:00:02Z',
          },
          {
            id: 'evt-1',
            type: 'service_status',
            source: 'obs',
            message: 'OBS connected again',
            createdAt: '2026-06-13T12:00:01Z',
          },
        ]}
      />,
    );

    expect(html).toContain('Event history');
    expect(html).toContain('custom command');
    expect(html).toContain('service status');
    expect(html).toContain('Mods called a command');
    expect(html).toContain('OBS connected again');
    expect(html).toContain('2026-06-13T12:00:02Z');
    expect(html).toContain('2026-06-13T12:00:01Z');
    expect(html.indexOf('custom command')).toBeLessThan(html.indexOf('service status'));
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

afterEach(() => {
  vi.unstubAllGlobals();
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

  it('fetches twitch interactions with protected GET headers', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ rules: { chatAliases: [], channelPointRules: [] } }),
    });
    vi.stubGlobal('fetch', fetchMock);

    await fetchTwitchInteractions({ baseUrl: 'http://example.test', operatorSecret: '  operator-secret  ' });
    await fetchTwitchInteractions('http://example.test', 'operator-secret');
    await fetchTwitchInteractions(undefined, 'operator-secret');

    expect(fetchMock).toHaveBeenNthCalledWith(1, 'http://example.test/api/twitch/interactions', {
      method: 'GET',
      headers: {
        Accept: 'application/json',
        'X-Overlay-Operator-Secret': 'operator-secret',
      },
    });
    expect(fetchMock).toHaveBeenNthCalledWith(2, 'http://example.test/api/twitch/interactions', {
      method: 'GET',
      headers: {
        Accept: 'application/json',
        'X-Overlay-Operator-Secret': 'operator-secret',
      },
    });
    expect(fetchMock).toHaveBeenNthCalledWith(3, 'http://localhost:8080/api/twitch/interactions', {
      method: 'GET',
      headers: {
        Accept: 'application/json',
        'X-Overlay-Operator-Secret': 'operator-secret',
      },
    });
  });

  it('shares request encoding and error detail handling across bot and twitch helpers', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ available: true, commandCount: 0, timerCount: 0, counterCount: 0, quoteCount: 0, auditCount: 0 }),
      })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ id: 'cmd-1' }) })
      .mockResolvedValueOnce({ ok: false, status: 418, text: async () => 'quota exceeded' });
    vi.stubGlobal('fetch', fetchMock);

    await fetchBotStatus('http://example.test', 'operator-secret');
    await saveBotCommand(
      { name: 'Go Live', enabled: true, response: '!', action: 'ticker', responseMode: 'overlay', roleRequired: 'viewer', cooldownSeconds: 30 },
      'http://example.test',
      'operator-secret',
    );

    await expect(deleteTwitchReward('reward-1', 'http://example.test', 'operator-secret')).rejects.toThrow(
      'Twitch reward delete failed: 418 — quota exceeded',
    );

    expect(fetchMock).toHaveBeenNthCalledWith(1, 'http://example.test/api/bot/status', {
      method: 'GET',
      headers: {
        Accept: 'application/json',
        'X-Overlay-Operator-Secret': 'operator-secret',
      },
    });
    expect(fetchMock).toHaveBeenNthCalledWith(2, 'http://example.test/api/bot/commands', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'X-Overlay-Operator-Secret': 'operator-secret',
      },
      body: JSON.stringify({
        name: 'Go Live',
        enabled: true,
        response: '!',
        action: 'ticker',
        responseMode: 'overlay',
        roleRequired: 'viewer',
        cooldownSeconds: 30,
      }),
    });
    expect(fetchMock).toHaveBeenNthCalledWith(3, 'http://example.test/api/twitch/rewards/reward-1', {
      method: 'DELETE',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'X-Overlay-Operator-Secret': 'operator-secret',
      },
      body: JSON.stringify({ confirm: true }),
    });
  });
});

describe('dashboard config loading', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('round-trips dashboard config through localStorage', () => {
    const localStorage = new Map<string, string>();
    const sessionStorage = new Map<string, string>();

    vi.stubGlobal('window', {
      localStorage: {
        getItem: (key: string) => localStorage.get(key) ?? null,
        setItem: (key: string, value: string) => localStorage.set(key, value),
        removeItem: (key: string) => localStorage.delete(key),
      },
      sessionStorage: {
        getItem: (key: string) => sessionStorage.get(key) ?? null,
        setItem: (key: string, value: string) => sessionStorage.set(key, value),
        removeItem: (key: string) => sessionStorage.delete(key),
      },
    });

    const config = normalizeDashboardConfig({ ...defaultDashboardConfig, activeAutomationProfileId: 'main-live' });
    saveDashboardConfig(config);

    expect(loadDashboardConfig().activeAutomationProfileId).toBe('main-live');
  });

  it('stores operator secret in sessionStorage only', () => {
    const localStorageSet = vi.fn();
    const sessionStorageSet = vi.fn();

    vi.stubGlobal('window', {
      localStorage: {
        getItem: () => null,
        setItem: localStorageSet,
        removeItem: vi.fn(),
      },
      sessionStorage: {
        getItem: () => 'operator-secret',
        setItem: sessionStorageSet,
        removeItem: vi.fn(),
      },
    });

    saveOperatorSecret('operator-secret');

    expect(loadOperatorSecret()).toBe('operator-secret');
    expect(localStorageSet).not.toHaveBeenCalled();
    expect(sessionStorageSet).toHaveBeenCalledWith('overlaid.operatorSecret.session.v1', 'operator-secret');
  });

  it('falls back to defaults for malformed dashboard config JSON', () => {
    vi.stubGlobal('window', {
      localStorage: {
        getItem: () => '{not-json',
        setItem: vi.fn(),
        removeItem: vi.fn(),
      },
      sessionStorage: {
        getItem: () => null,
        setItem: vi.fn(),
        removeItem: vi.fn(),
      },
    });

    expect(loadDashboardConfig().activeAutomationProfileId).toBe(defaultDashboardConfig.activeAutomationProfileId);
  });

  it('adds default automation profiles to old local storage configs', () => {
    vi.stubGlobal('window', {
      localStorage: {
        getItem: () => JSON.stringify({ quickActions: defaultDashboardConfig.quickActions.slice(0, 1) }),
      },
    });

    const config = loadDashboardConfig();

    expect(config.automationProfiles.map((profile) => profile.id)).toEqual(['full-console', 'main-live', 'brb-ops']);
    expect(config.activeAutomationProfileId).toBe('full-console');
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

describe('automation profile helpers', () => {
  it('filters main live quick actions in profile order', () => {
    const config = normalizeDashboardConfig({
      ...defaultDashboardConfig,
      activeAutomationProfileId: 'main-live',
    });

    const { actions, warnings } = selectQuickActionsForProfile(config);

    expect(actions.map((action) => action.id)).toEqual(['sequence-go-live-main', 'preset-main', 'refresh-alerts', 'sample-alert', 'hide-brb']);
    expect(warnings).toEqual([]);
  });

  it('warns on missing quick action ids and falls back to all actions', () => {
    const config = normalizeDashboardConfig({
      ...defaultDashboardConfig,
      activeAutomationProfileId: 'missing-profile',
      automationProfiles: [
        ...defaultDashboardConfig.automationProfiles,
        {
          id: 'missing-profile',
          title: 'Broken profile',
          description: 'Only missing ids.',
          quickActionIds: ['missing-a', 'missing-b'],
          streamPresetIds: ['missing-c'],
        },
      ],
    });

    const { actions, warnings } = selectQuickActionsForProfile(config);

    expect(actions.map((action) => action.id)).toEqual(defaultDashboardConfig.quickActions.map((action) => action.id));
    expect(warnings).toEqual([
      { profileId: 'missing-profile', message: 'Missing quick action: missing-a' },
      { profileId: 'missing-profile', message: 'Missing quick action: missing-b' },
      { profileId: 'missing-profile', message: 'No valid quick actions selected; falling back to all quick actions.' },
    ]);
  });

  it('round-trips dashboard config without operator secret', () => {
    const raw = JSON.stringify({
      ...defaultDashboardConfig,
      operatorSecret: 'super-secret',
      activeAutomationProfileId: 'main-live',
      unknownField: 'ignored',
    });

    const imported = importDashboardConfig(raw);
    const exported = exportDashboardConfig(imported);
    const parsed = JSON.parse(exported) as Record<string, unknown>;

    expect(exported).not.toContain('super-secret');
    expect(parsed).not.toHaveProperty('operatorSecret');
    expect(parsed).not.toHaveProperty('unknownField');
    expect(parsed.activeAutomationProfileId).toBe('main-live');
    expect(getActiveAutomationProfile(imported).id).toBe('main-live');
  });

  it('drops nested unknown fields from imported dashboard config', () => {
    const raw = JSON.stringify({
      ...defaultDashboardConfig,
      quickActions: [
        {
          id: 'scene-custom',
          label: 'Custom scene',
          kind: 'setScene',
          sceneName: 'Scene A',
          operatorSecret: 'quick-action-secret',
          nested: { token: 'hidden' },
        },
        {
          id: 'sequence-custom',
          label: 'Custom sequence',
          kind: 'sequence',
          actionIds: ['scene-custom'],
          stopOnError: true,
          confirm: false,
          operatorSecret: 'sequence-secret',
        },
      ],
      automationProfiles: [
        {
          id: 'custom-profile',
          title: 'Custom profile',
          description: 'Custom description',
          quickActionIds: ['scene-custom'],
          streamPresetIds: ['main'],
          operatorSecret: 'profile-secret',
          nested: { token: 'hidden' },
        },
      ],
      streamPresets: [
        {
          id: 'main',
          title: 'Main scene',
          sources: ['alerts'],
          operatorSecret: 'preset-secret',
          extra: 'ignored',
        },
      ],
      setupChecklist: [
        {
          id: 'check-1',
          title: 'Checklist item',
          description: 'Custom checklist item',
          completed: true,
          operatorSecret: 'check-secret',
          extra: 'ignored',
        },
      ],
      controlWidgets: [
        {
          id: 'widget-1',
          title: 'Widget',
          enabled: true,
          order: 2,
          compact: false,
          operatorSecret: 'widget-secret',
          extra: 'ignored',
        },
      ],
      display: {
        alertMaxVisible: 4,
        alertDedupeSeconds: 12,
        chatMaxVisible: 6,
        tickerMaxItems: 9,
        operatorSecret: 'display-secret',
        extra: 'ignored',
      },
    });

    const exported = exportDashboardConfig(importDashboardConfig(raw));
    const parsed = JSON.parse(exported) as {
      quickActions: Array<Record<string, unknown>>;
      automationProfiles: Array<Record<string, unknown>>;
      streamPresets: Array<Record<string, unknown>>;
      setupChecklist: Array<Record<string, unknown>>;
      controlWidgets: Array<Record<string, unknown>>;
      display: Record<string, unknown>;
    };

    expect(exported).not.toContain('operatorSecret');
    expect(exported).not.toContain('nested');
    expect(exported).not.toContain('extra');
    expect(parsed.quickActions[0]).toEqual({ id: 'scene-custom', label: 'Custom scene', kind: 'setScene', sceneName: 'Scene A' });
    expect(parsed.quickActions[1]).toEqual({
      id: 'sequence-custom',
      label: 'Custom sequence',
      kind: 'sequence',
      actionIds: ['scene-custom'],
      stopOnError: true,
      confirm: false,
    });
    expect(Object.keys(parsed.automationProfiles[0]).sort()).toEqual(['description', 'id', 'quickActionIds', 'streamPresetIds', 'title']);
    expect(Object.keys(parsed.streamPresets[0]).sort()).toEqual(['id', 'sources', 'title']);
    expect(Object.keys(parsed.setupChecklist[0]).sort()).toEqual(['completed', 'description', 'id', 'title']);
    expect(Object.keys(parsed.controlWidgets[0]).sort()).toEqual(['compact', 'enabled', 'id', 'order', 'title']);
    expect(Object.keys(parsed.display).sort()).toEqual(['alertDedupeSeconds', 'alertMaxVisible', 'chatMaxVisible', 'tickerMaxItems']);
  });
});

describe('quick action sequence utilities', () => {
  it('models confirmable action arming without DOM clicks', () => {
    expect(getConfirmableActionLabel('Clear overlays', false)).toBe('Clear overlays');
    expect(getConfirmableActionLabel('Clear overlays', true)).toBe('Confirm Clear overlays');
    expect(nextConfirmableActionState({ confirm: true, armed: false })).toEqual({ armed: true, shouldRun: false });
    expect(nextConfirmableActionState({ confirm: true, armed: true })).toEqual({ armed: false, shouldRun: true });
    expect(nextConfirmableActionState({ confirm: false, armed: false })).toEqual({ armed: false, shouldRun: true });
  });

  it('filters visible quick actions using the full catalog', () => {
    const actions: QuickAction[] = [
      { id: 'seq', label: 'Visible sequence', kind: 'sequence', actionIds: ['hidden-child'], stopOnError: true },
      { id: 'empty', label: 'Empty sequence', kind: 'sequence', actionIds: ['missing'], stopOnError: true },
      { id: 'alert', label: 'Sample alert', kind: 'sendAlert', title: 'A', message: 'B', severity: 'success' },
    ];
    const catalog: QuickAction[] = [{ id: 'hidden-child', label: 'Hidden child', kind: 'setScene', sceneName: 'Main' }, ...actions];

    expect(getVisibleQuickActions(actions, catalog).map((action) => action.id)).toEqual(['seq', 'alert']);
  });

  it('formats sequence run warnings and failures', () => {
    expect(formatQuickActionRunResult({ ran: 1, warnings: ['Missing action: old'], failures: [] })).toBe(
      'Action completed with warnings: Missing action: old',
    );
    expect(() => formatQuickActionRunResult({ ran: 0, warnings: ['Missing action: old'], failures: [] })).toThrow(
      'Sequence has no runnable actions; Missing action: old',
    );
    expect(() => formatQuickActionRunResult({ ran: 2, warnings: ['Missing action: old'], failures: ['Go Main: boom'] })).toThrow(
      'Go Main: boom; Missing action: old',
    );
  });

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

  it('explains disabled quick actions when operator secret is missing', () => {
    const html = renderToString(
      <QuickActionRunner
        actions={[{ id: 'a', label: 'Run Main', kind: 'setScene', sceneName: '40_MAIN' }]}
        apiBaseUrl="http://localhost:8080"
        operatorSecret=""
        isRunning={false}
        runAction={() => {
          throw new Error('unused');
        }}
      />,
    );

    expect(html).toContain('Operator secret is missing');
    expect(html).toContain('disabled');
  });

  it('renders sequences when children exist only in the action catalog', () => {
    const actions: QuickAction[] = [{ id: 'seq', label: 'Visible Sequence', kind: 'sequence', actionIds: ['child-only'], stopOnError: true }];
    const actionCatalog: QuickAction[] = [
      { id: 'child-only', label: 'Hidden Child', kind: 'setScene', sceneName: 'Main' },
      actions[0],
    ];

    const html = renderToString(
      <QuickActionRunner
        actions={actions}
        actionCatalog={actionCatalog}
        apiBaseUrl="http://localhost:8080"
        operatorSecret="secret"
        isRunning={false}
        runAction={() => {
          throw new Error('unused');
        }}
      />,
    );

    expect(html).toContain('Visible Sequence');
    expect(html).not.toContain('Hidden Child');
  });

  it('highlights the latest operator action in the session log', () => {
    const html = renderToString(
      <SessionLogPanel
        entries={[
          { id: '1', label: 'Refresh Alerts', status: 'success', message: 'Action completed. (120ms)', createdAt: '2026-06-15T00:00:00Z' },
        ]}
      />,
    );

    expect(html).toContain('Latest action');
    expect(html).toContain('success');
    expect(html).toContain('Refresh Alerts');
    expect(html).toContain('Action completed. (120ms)');
  });

  it('validates quick-action scene and input names against OBS', () => {
    const issues = validateQuickActionsAgainstOBS(
      [
        { id: 'main', label: 'Go Main', kind: 'setScene', sceneName: 'Missing Scene' },
        { id: 'alerts', label: 'Refresh Alerts', kind: 'refreshSource', inputName: 'ALERTS' },
      ],
      ['40_MAIN'],
      ['ALERTS'],
    );

    expect(issues).toEqual([
      { actionId: 'main', label: 'Go Main', field: 'sceneName', value: 'Missing Scene', message: 'Missing OBS scene: Missing Scene' },
    ]);
  });

  it('formats session reports for export', () => {
    const report: StreamSessionReport = {
      session: { id: 'abcdef123456', title: 'Preflight rehearsal', kind: 'rehearsal', startedAt: '2026-06-15T00:00:00Z', eventCount: 1 },
      events: [{ id: 'e1', type: 'alert', source: 'test', title: 'Sample alert', message: '', severity: 'success', createdAt: '2026-06-15T00:01:00Z' }],
      snapshots: [{ id: 's1', sessionId: 'abcdef123456', phase: 'start', status: 'ok', checks: [], capturedAt: '2026-06-15T00:00:00Z' }],
    };

    expect(formatSessionReportMarkdown(report)).toContain('# Preflight rehearsal');
    expect(formatSessionReportMarkdown(report)).toContain('## Events');
    expect(sessionReportFilename(report, 'md')).toBe('preflight-rehearsal-abcdef12.md');
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

describe('BRB payload helpers', () => {
  it('parses and trims BRB items', () => {
    expect(parseBRBItems(' Hydrate \n\n Stretch \n')).toEqual(['Hydrate', 'Stretch']);
  });

  it('builds trimmed BRB show payloads with fallbacks', () => {
    expect(buildShowBRBPayload({ title: ' ', message: ' ', eta: ' 5 min ', itemsText: ' A \n B ' })).toEqual({
      active: true,
      title: 'Be right back',
      message: 'Short intermission. Stream resumes soon.',
      eta: '5 min',
      items: ['A', 'B'],
    });
  });

  it('builds the inactive BRB hide payload', () => {
    expect(buildHideBRBPayload()).toEqual({
      active: false,
      title: 'Be right back',
      message: 'Short intermission. Stream resumes soon.',
      eta: '5 min',
      items: ['Hydrate', 'Stretch', 'Chat stays live'],
      data: { preset: 'sample-hide' },
    });
  });
});

describe('twitch interaction rule helpers', () => {
  it('normalizes imported Twitch interaction rules safely', () => {
    const rules = normalizeImportedTwitchInteractionRules({
      chatAliases: [{ alias: 'panic', command: 'unsafe', enabled: true, operatorSecret: 'hidden' }],
      channelPointRules: [{ rewardTitle: 'Hydrate', action: 'unsafe', title: 'T', message: 'M', enabled: true, nested: { token: 'hidden' } }],
    });

    expect(rules).toEqual({
      chatAliases: [{ alias: 'panic', command: 'alert', enabled: true }],
      channelPointRules: [{ rewardTitle: 'Hydrate', action: 'ignore', title: 'T', message: 'M', enabled: true }],
    });
    expect(JSON.stringify(rules)).not.toContain('operatorSecret');
    expect(JSON.stringify(rules)).not.toContain('nested');
  });

  it('rejects invalid Twitch interaction rule imports', () => {
    expect(() => normalizeImportedTwitchInteractionRules({ chatAliases: [] })).toThrow('invalid rules JSON');
  });

  it('updates Twitch interaction rules immutably', () => {
    const original = {
      chatAliases: [{ alias: 'panic', command: 'alert' as const, enabled: true }],
      channelPointRules: [{ rewardTitle: 'Hydrate', action: 'ignore' as const, enabled: true }],
    };

    const updatedAliasRules = updateTwitchAliasRule(original, 0, { command: 'brb' });
    const updatedRewardRules = updateTwitchRewardRule(original, 0, { action: 'alert' });

    expect(updatedAliasRules.chatAliases[0].command).toBe('brb');
    expect(updatedRewardRules.channelPointRules[0].action).toBe('alert');
    expect(original.chatAliases[0].command).toBe('alert');
    expect(original.channelPointRules[0].action).toBe('ignore');
    expect(updatedAliasRules.chatAliases[0]).not.toBe(original.chatAliases[0]);
    expect(updatedRewardRules.channelPointRules[0]).not.toBe(original.channelPointRules[0]);
  });

  it('finds enabled channel point rules for reward queue notes', () => {
    const rule = findMatchingChannelPointRule(
      [
        { rewardTitle: 'Hydrate', action: 'ignore', enabled: false, fulfillmentNote: 'disabled note' },
        { rewardTitle: ' Hydrate ', action: 'alert', enabled: true, fulfillmentNote: 'Check the water timer.' },
      ],
      'hydrate',
    );

    expect(rule?.fulfillmentNote).toBe('Check the water timer.');
    expect(findMatchingChannelPointRule([], 'hydrate')).toBeUndefined();
  });
});
