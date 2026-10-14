async function add(url, title = '') {
  const { api, token, shelfId, segmentId } = await chrome.storage.local.get(['api', 'token', 'shelfId', 'segmentId']);
  if (!api || !token) { chrome.runtime.openOptionsPage(); return; }
  try {
    const response = await fetch(new URL('/api/newsroom/quick-add', api), { method: 'POST', credentials: 'omit', headers: { 'Content-Type': 'application/json', 'X-Newsroom-Add-Token': token }, body: JSON.stringify({ url, title, shelfId: shelfId || '', segmentId: segmentId || '' }) });
    chrome.action.setBadgeText({ text: response.ok ? '✓' : '!' });
    chrome.action.setBadgeBackgroundColor({ color: response.ok ? '#3a7d44' : '#b23a2e' });
  } catch { chrome.action.setBadgeText({ text: '!' }); }
  setTimeout(() => chrome.action.setBadgeText({ text: '' }), 2500);
}
chrome.action.onClicked.addListener((tab) => { if (tab.url) void add(tab.url, tab.title || ''); });
chrome.runtime.onInstalled.addListener(() => { chrome.contextMenus.create({ id: 'overlaid-add', title: 'Add to Over-Laid Newsroom', contexts: ['link', 'page'] }); });
chrome.contextMenus.onClicked.addListener((info, tab) => { if (info.menuItemId === 'overlaid-add') void add(info.linkUrl || info.pageUrl || tab?.url, info.linkUrl ? '' : tab?.title || ''); });
