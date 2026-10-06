import {
  fetchBotStatus,
  fetchBotAudit,
  fetchBotCommands,
  fetchBotCounters,
  fetchBotQuotes,
  fetchBotAPIVariables,
  fetchBotModerationRules,
} from '../../../lib/api';
import type { BotPageState } from './types';

function panelResult<T extends { loadError?: string }>(result: PromiseSettledResult<T>, fallback: T): T {
  if (result.status === 'fulfilled') return result.value;
  const loadError = result.reason instanceof Error ? result.reason.message : 'Request unavailable';
  return { ...fallback, loadError };
}

// Loading one operation must not hide data from the other bot panels.
export async function loadBotWorkspace(apiBaseUrl: string, operatorSecret: string): Promise<BotPageState> {
  const [status, audit, commands, counters, quotes, apiVariables, moderation] = await Promise.allSettled([
    fetchBotStatus(apiBaseUrl, operatorSecret),
    fetchBotAudit(25, apiBaseUrl, operatorSecret),
    fetchBotCommands(apiBaseUrl, operatorSecret),
    fetchBotCounters(apiBaseUrl, operatorSecret),
    fetchBotQuotes(apiBaseUrl, operatorSecret),
    fetchBotAPIVariables(apiBaseUrl, operatorSecret),
    fetchBotModerationRules(apiBaseUrl, operatorSecret),
  ]);
  return {
    status: panelResult(status, { available: false, commandCount: 0, timerCount: 0, counterCount: 0, quoteCount: 0, auditCount: 0 }),
    audit: panelResult(audit, { events: [] }),
    commands: panelResult(commands, { commands: [] }),
    counters: panelResult(counters, { counters: [] }),
    quotes: panelResult(quotes, { quotes: [] }),
    apiVariables: panelResult(apiVariables, { variables: [] }),
    moderation: panelResult(moderation, { rules: [] }),
    moderationDryRun: null,
    loading: false,
    error: '',
  };
}
