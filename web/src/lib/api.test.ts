import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  clearOverlays, endStreamSession, fetchBuildInfo, fetchDashboardConfig,
  fetchEventHistory, fetchOBSInputs, fetchOBSSceneItems, fetchOBSScenes,
  fetchOBSStatus, fetchReadiness, fetchStreamSession, fetchStreamSessionReport,
  fetchTwitchLoginURL, fetchTwitchStatus, sendWebhook, setOBSScene,
  startStreamSession,
} from './api';

const base = 'http://example.test';
const operator = '  operator-fixture  ';

afterEach(() => vi.unstubAllGlobals());

describe('API operation transport', () => {
  it.each([
    ['Twitch status', '/api/auth/twitch/status', () => fetchTwitchStatus(base), false],
    ['OBS status', '/api/obs/status', () => fetchOBSStatus(base), false],
    ['readiness', '/readyz', () => fetchReadiness(base), false],
    ['version', '/version', () => fetchBuildInfo(base), false],
    ['history', '/api/events/history?limit=250', () => fetchEventHistory(999, base), false],
    ['session', '/api/stream/session', () => fetchStreamSession(base, operator), true],
    ['session report', '/api/stream/session/session%2Fid?limit=1', () => fetchStreamSessionReport('session/id', 0, base, operator), true],
    ['dashboard config', '/api/dashboard/config', () => fetchDashboardConfig(base, operator), true],
    ['OBS scenes', '/api/obs/scenes', () => fetchOBSScenes(base, operator), true],
    ['OBS inputs', '/api/obs/inputs', () => fetchOBSInputs(base, operator), true],
    ['OBS items', '/api/obs/scene-items?sceneName=Scene%20%26%20one', () => fetchOBSSceneItems(' Scene & one ', base, operator), true],
  ] as const)('loads %s with the correct read credentials and encoded parameters', async (_name, path, load, protectedRead) => {
    const value = { fixture: true };
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => value });
    vi.stubGlobal('fetch', fetchMock);
    await expect(load()).resolves.toEqual(value);
    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock).toHaveBeenCalledWith(base + path, {
      method: 'GET',
      headers: protectedRead ? { Accept: 'application/json', 'X-Overlay-Operator-Secret': 'operator-fixture' } : { Accept: 'application/json' },
    });
  });

  it('retains JSON receipts for session and login writes', async () => {
    const session = { id: 'session-1', title: 'Rehearsal' };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => session })
      .mockResolvedValueOnce({ ok: true, json: async () => session })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ authorizeUrl: 'https://example.test/login' }) });
    vi.stubGlobal('fetch', fetchMock);
    await expect(startStreamSession({ title: 'Rehearsal', kind: 'rehearsal' }, base, operator)).resolves.toEqual(session);
    await expect(endStreamSession(base, operator)).resolves.toEqual(session);
    await expect(fetchTwitchLoginURL(base, operator)).resolves.toEqual({ authorizeUrl: 'https://example.test/login' });
    expect(fetchMock.mock.calls.map(([url, options]) => [url, options.method, JSON.parse(options.body)])).toEqual([
      [base + '/api/stream/session/start', 'POST', { title: 'Rehearsal', kind: 'rehearsal' }],
      [base + '/api/stream/session/end', 'POST', {}],
      [base + '/api/auth/twitch/login-url', 'POST', {}],
    ]);
  });

  it('accepts empty write responses and keeps webhook credentials separate', async () => {
    const json = vi.fn().mockRejectedValue(new Error('empty body'));
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 204, json });
    vi.stubGlobal('fetch', fetchMock);
    await setOBSScene('Scene', base, operator);
    await clearOverlays(base, operator);
    await sendWebhook('source/id', { title: 'Fixture', message: 'Only a mock', severity: 'info' }, base, ' webhook-fixture ');
    expect(json).not.toHaveBeenCalled();
    expect(fetchMock.mock.calls[1]).toEqual([base + '/api/events/clear', { method: 'POST', headers: { Accept: 'application/json', 'X-Overlay-Operator-Secret': 'operator-fixture' } }]);
    expect(fetchMock.mock.calls[2]).toEqual([base + '/api/integrations/webhook/source%2Fid', {
      method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-Overlay-Webhook-Secret': 'webhook-fixture' },
      body: JSON.stringify({ title: 'Fixture', message: 'Only a mock', severity: 'info' }),
    }]);
  });

  it('reports backend details consistently for protected reads and writes', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 503, text: async () => '  OBS is disconnected\n' }));
    await expect(fetchOBSScenes(base, operator)).rejects.toThrow('OBS scenes failed: 503 — OBS is disconnected');
    await expect(setOBSScene('Scene', base, operator)).rejects.toThrow('/api/obs/scene failed: 503 — OBS is disconnected');
    await expect(fetchStreamSessionReport('one', 50, base, operator)).rejects.toThrow('Stream session report failed: 503 — OBS is disconnected');
  });

  it('reports the failed operation when successful HTTP returns malformed JSON', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => { throw new SyntaxError('private raw response'); } }));
    await expect(fetchReadiness(base)).rejects.toThrow('Readiness failed: invalid JSON response');
    await expect(startStreamSession({ title: 'Fixture', kind: 'rehearsal' }, base, operator)).rejects.toThrow('/api/stream/session/start failed: invalid JSON response');
  });

  it('retains HTTP status if reading the error body fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 401, text: async () => { throw new Error('closed response'); } }));
    await expect(fetchDashboardConfig(base, operator)).rejects.toThrow('Dashboard config fetch failed: 401');
  });
});
