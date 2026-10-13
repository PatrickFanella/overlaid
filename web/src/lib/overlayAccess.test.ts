import { afterEach, expect, it, vi } from 'vitest';
import { buildOverlaySourceUrl } from './overlayRoutes';
import { getOverlayEndpointUrl, getOverlayStreamUrl } from './eventStream';

afterEach(() => vi.unstubAllGlobals());
it('encodes only the overlay key in source, stream and display URLs', () => {
  vi.stubGlobal('window', { location: { origin: 'https://desk.example', search: '?key=overlay%2B%26&apiBaseUrl=https%3A%2F%2Fapi.example' } });
  const source = new URL(buildOverlaySourceUrl('/overlay/chat', 'https://api.example', 'overlay+&'));
  expect(source.searchParams.get('key')).toBe('overlay+&');
  expect(source.searchParams.get('apiBaseUrl')).toBe('https://api.example');
  expect(new URL(getOverlayStreamUrl()).searchParams.get('key')).toBe('overlay+&');
  expect(new URL(getOverlayEndpointUrl('/api/overlay/display')).searchParams.get('key')).toBe('overlay+&');
});
it('keeps local URLs usable without a configured token', () => {
  vi.stubGlobal('window', { location: { origin: 'http://localhost:5173', search: '' } });
  expect(buildOverlaySourceUrl('/overlay/chat', 'http://localhost:8080')).toBe('http://localhost:5173/overlay/chat');
  expect(getOverlayStreamUrl()).toBe('http://localhost:8080/api/events/stream');
});

it('rejects token retrieval on 401 while using the operator header', async () => {
  const { fetchOverlayToken } = await import('./api');
  const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 401, text: async () => 'operator authorization required' });
  vi.stubGlobal('fetch', fetchMock);
  await expect(fetchOverlayToken('https://api.example', 'operator-fixture')).rejects.toThrow('401');
  expect(fetchMock).toHaveBeenCalledWith('https://api.example/api/dashboard/overlay-token', {
    method: 'GET', credentials: 'include', headers: { Accept: 'application/json', 'X-Overlay-Operator-Secret': 'operator-fixture' },
  });
});
