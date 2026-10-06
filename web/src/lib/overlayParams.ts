// Browser-source query parameters shared by every overlay route.
//
//   ?demo=1          play sample events locally instead of waiting for the API
//   ?status=1        always show the connection pip (it otherwise hides while live)
//   ?apiBaseUrl=...  handled by eventStream.ts

export function getOverlayParam(name: string) {
  if (typeof window === 'undefined') {
    return '';
  }

  return new URLSearchParams(window.location.search).get(name)?.trim() ?? '';
}

export function isOverlayFlagSet(name: string) {
  const value = getOverlayParam(name).toLowerCase();
  return value === '1' || value === 'true' || value === 'yes';
}

export function isOverlayDemoMode() {
  return isOverlayFlagSet('demo');
}

export function getOverlayNumberParam(name: string, fallback: number) {
  const value = Number(getOverlayParam(name));
  return Number.isFinite(value) && getOverlayParam(name) !== '' ? value : fallback;
}
