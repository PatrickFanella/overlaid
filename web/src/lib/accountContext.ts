export type AccountContext = { deploymentMode?: 'selfhost' | 'hosted'; tenantId?: string; csrf?: string };
const key = 'overlaid.account.context.v1';

export function loadAccountContext(): AccountContext {
  if (typeof window === 'undefined') return {};
  try {
    const value: unknown = JSON.parse(window.sessionStorage.getItem(key) || '{}');
    if (typeof value !== 'object' || value === null) return {};
    return value as AccountContext;
  } catch { return {}; }
}

export function saveAccountContext(value: AccountContext) {
  if (typeof window !== 'undefined') window.sessionStorage.setItem(key, JSON.stringify(value));
}

export function clearAccountContext() {
  if (typeof window !== 'undefined') window.sessionStorage.removeItem(key);
}

export function tenantStorageKey(base: string) {
  const tenant = loadAccountContext().tenantId;
  return tenant ? `${base}.tenant.${tenant}` : base;
}

export function accountHeaders() {
  const context = loadAccountContext();
  const headers: Record<string, string> = {};
  if (context.tenantId) headers['X-Overlaid-Tenant'] = context.tenantId;
  let csrf = context.csrf;
  if (!csrf && typeof document !== 'undefined') csrf = document.cookie.split('; ').find((item) => item.startsWith('overlaid_csrf='))?.slice('overlaid_csrf='.length);
  if (csrf) headers['X-Overlaid-CSRF'] = csrf;
  return headers;
}
