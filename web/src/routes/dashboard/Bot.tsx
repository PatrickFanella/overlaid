import { useEffect, useRef, useState } from 'react';
import { fetchBotAudit, fetchBotStatus } from '../../lib/api';
import { loadOperatorSecret } from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { BotStatusSummary } from './bot/BotStatusSummary';
import { loadBotWorkspace } from './bot/loadBotWorkspace';
import type { BotPageState } from './bot/types';
import { CustomizablePanelLayout } from './components/CustomizablePanelLayout';
import type { PanelLayoutItem } from '../../lib/dashboardLayout';
import { loadDashboardConfig, saveDashboardConfig } from '../../lib/dashboardConfig';
import { BotCommandsPanel } from './bot/BotCommandsPanel';
import { BotCommunityPanel } from './bot/BotCommunityPanel';
import { BotAPIVariablesPanel } from './bot/BotAPIVariablesPanel';
import { BotModerationPanel } from './bot/BotModerationPanel';
import { BotTimersPanel } from './bot/BotTimersPanel';
import { useBotTimers } from './bot/useBotTimers';
import { useBotCommunity } from './bot/useBotCommunity';
import { useBotCommands } from './bot/useBotCommands';
import { useBotAPIVariables } from './bot/useBotAPIVariables';
import { useBotModeration } from './bot/useBotModeration';

export function BotWorkspace() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const operatorSecret = loadOperatorSecret();
  const [config, setConfig] = useState(() => loadDashboardConfig());
  const workspaceGeneration = useRef(0);
  const summaryRequest = useRef(0);
  const [state, setState] = useState<BotPageState>({ status: null, audit: null, loading: false, error: '' });
  const timers = useBotTimers(apiBaseUrl, operatorSecret, refreshBotSummary);
  const community = useBotCommunity(apiBaseUrl, operatorSecret, refreshBotSummary);
  const commands = useBotCommands(apiBaseUrl, operatorSecret, refreshBotSummary);
  const apiVariables = useBotAPIVariables(apiBaseUrl, operatorSecret, refreshBotSummary);
  const moderation = useBotModeration(apiBaseUrl, operatorSecret, refreshBotSummary);

  function updateBotPanelLayout(items: PanelLayoutItem[]) {
    setConfig((current) => {
      const next = { ...current, panelLayouts: { ...current.panelLayouts, bot: items } };
      saveDashboardConfig(next);
      return next;
    });
  }

  useEffect(() => {
    let cancelled = false;
    workspaceGeneration.current += 1;
    async function load() {
      setState((current) => ({ ...current, status: null, audit: null, loading: true, error: '' }));
      const loaded = await loadBotWorkspace(apiBaseUrl, operatorSecret);
      if (!cancelled) {
        setState((current) => ({ ...loaded, status: current.status ?? loaded.status, audit: current.audit ?? loaded.audit }));
      }
    }
    void load();
    return () => {
      cancelled = true;
      workspaceGeneration.current += 1;
    };
  }, [apiBaseUrl, operatorSecret]);

  async function refreshBotSummary() {
    const generation = workspaceGeneration.current;
    const request = ++summaryRequest.current;
    const [status, audit] = await Promise.all([fetchBotStatus(apiBaseUrl, operatorSecret), fetchBotAudit(25, apiBaseUrl, operatorSecret)]);
    if (generation === workspaceGeneration.current && request === summaryRequest.current) {
      setState((current) => ({ ...current, status, audit }));
    }
  }

  return (
    <>
      <BotStatusSummary status={state.status} audit={state.audit} />

      <CustomizablePanelLayout
        page="bot"
        items={config.panelLayouts.bot}
        onChange={updateBotPanelLayout}
        description="Reorder or hide safe bot panels. Moderation guardrails stay locked."
        panels={[
          {
            id: 'commands',
            node: (
              <BotCommandsPanel {...commands} />
            ),
          },
          {
            id: 'community',
            node: (
              <BotCommunityPanel {...community} />
            ),
          },
          {
            id: 'api-variables',
            node: (
              <BotAPIVariablesPanel {...apiVariables} />
            ),
          },
          {
            id: 'timers',
            node: (
              <BotTimersPanel {...timers} />
            ),
          },
        ]}
      />

      <BotModerationPanel {...moderation} />
    </>
  );
}

