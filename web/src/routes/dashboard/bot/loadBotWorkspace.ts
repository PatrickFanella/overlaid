import {
  fetchBotStatus,
  fetchBotAudit,
} from '../../../lib/api';
import type { BotPageState } from './types';

function panelResult<T extends { loadError?: string }>(result: PromiseSettledResult<T>, fallback: T): T {
  if (result.status === 'fulfilled') return result.value;
  const loadError = result.reason instanceof Error ? result.reason.message : 'Request unavailable';
  return { ...fallback, loadError };
}

// Loading one operation must not hide data from the other bot panels.
export async function loadBotWorkspace(apiBaseUrl: string, operatorSecret: string): Promise<BotPageState> {
  const [status, audit] = await Promise.allSettled([
    fetchBotStatus(apiBaseUrl, operatorSecret),
    fetchBotAudit(25, apiBaseUrl, operatorSecret),
  ]);
  return {
    status: panelResult(status, { available: false, commandCount: 0, timerCount: 0, counterCount: 0, quoteCount: 0, auditCount: 0 }),
    audit: panelResult(audit, { events: [] }),
    loading: false,
    error: '',
  };
}
