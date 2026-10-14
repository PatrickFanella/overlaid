const api = document.querySelector('#api'), token = document.querySelector('#token'), status = document.querySelector('#status'), shelf = document.querySelector('#shelf'), segment = document.querySelector('#segment');
chrome.storage.local.get(['api', 'token']).then((config) => { api.value = config.api || ''; token.value = config.token || ''; });
document.querySelector('#settings').addEventListener('submit', async (event) => {
  event.preventDefault(); let origin;
  try { const url = new URL(api.value); if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password) throw new Error(); origin = url.origin; } catch { status.textContent = 'Enter an HTTP(S) API origin without credentials.'; return; }
  const granted = await chrome.permissions.request({ origins: [`${origin}/*`] });
  if (!granted) { status.textContent = 'API access was not granted.'; return; }
  await chrome.storage.local.set({ api: origin, token: token.value.trim() }); status.textContent = 'Connection saved. Click the toolbar button or right-click a link to add it.'; await targets();
});
async function targets() {
  try {
    const config = await chrome.storage.local.get(['api', 'token', 'shelfId', 'segmentId']);
    if (!config.api || !config.token) return;
    const response = await fetch(new URL('/api/newsroom/quick-add', config.api), { credentials: 'omit', headers: { 'X-Newsroom-Add-Token': config.token } }); if (!response.ok) throw new Error(); const data = await response.json();
    shelf.replaceChildren(new Option('Live deck', ''), ...data.shelves.map((s) => new Option(s.name, s.id))); segment.replaceChildren(new Option('Ungrouped', ''), ...data.segments.map((s) => new Option(s.name, s.id))); shelf.value = config.shelfId || ''; segment.value = config.segmentId || '';
  } catch { status.textContent = 'Could not load destinations. Check the API origin and add token.'; }
}
document.querySelector('#targets').addEventListener('click', targets);
shelf.addEventListener('change', () => chrome.storage.local.set({ shelfId: shelf.value })); segment.addEventListener('change', () => chrome.storage.local.set({ segmentId: segment.value }));
