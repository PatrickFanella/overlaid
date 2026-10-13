import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { requestJSON } from '../../lib/apiRequest';
import { clearAccountContext, loadAccountContext, saveAccountContext } from '../../lib/accountContext';
import { loadOperatorSecret } from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';

export type Tenant = { id: string; name: string; slug: string; role: string; plan: string; retentionDays: number };
export type Account = { authenticated: boolean; deploymentMode: 'selfhost' | 'hosted'; accountId?: string; activeTenantId?: string; tenants: Tenant[]; productName?: string; supportContact?: string; csrfToken?: string };
const fallback: Account = { authenticated: false, deploymentMode: 'selfhost', tenants: [] };
const AccountContext = createContext<{ account: Account; loading: boolean; refresh: () => Promise<void> }>({ account: fallback, loading: true, refresh: async () => {} });

export function AccountProvider({ children }: { children: ReactNode }) {
  const [account, setAccount] = useState<Account>({ ...fallback, deploymentMode: loadAccountContext().deploymentMode || 'selfhost' });
  const [loading, setLoading] = useState(true);
  async function refresh() {
    try {
      const response = await requestJSON<Account>('/api/account', { method: 'GET', errorLabel: 'Account', operatorSecret: loadOperatorSecret() });
      if (response.deploymentMode === 'hosted' || response.deploymentMode === 'selfhost') {
        const normalized = { ...response, tenants: response.tenants || [] };
        saveAccountContext({ deploymentMode: normalized.deploymentMode, tenantId: normalized.activeTenantId, csrf: normalized.csrfToken });
        setAccount(normalized);
      }
    } catch {
      if (loadAccountContext().deploymentMode === 'hosted') {
        setAccount({ ...fallback, deploymentMode: 'hosted' });
        saveAccountContext({ deploymentMode: 'hosted' });
      }
      // Direct legacy/no-database development keeps its existing operator flow.
    } finally { setLoading(false); }
  }
  useEffect(() => { void refresh(); }, []);
  return <AccountContext.Provider value={{ account, loading, refresh }}>{children}</AccountContext.Provider>;
}
export function useAccount() { return useContext(AccountContext); }
export function loginURL() { return new URL('/api/account/login/twitch', getOverlayApiBaseUrl()).toString(); }
export async function signOut() {
  await requestJSON('/api/account/logout', { method: 'POST', body: {}, errorLabel: 'Sign out', expectJson: false });
  clearAccountContext(); window.location.href = '/dashboard/account';
}
