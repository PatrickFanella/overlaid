import { useEffect, useRef, useState } from 'react';
import {
  deleteBotModerationRule,
  dryRunBotModeration,
  dryRunBotNuke,
  executeBotNuke,
  executeBotModerationAction,
  fetchBotAudit,
  fetchBotModerationRules,
  fetchBotStatus,
  rollbackBotNukeRadiation,
  saveBotModerationRule,
  type BotNukeDryRunResponse,
  type BotNukeExecuteResponse,
  type BotModerationRule,
} from '../../lib/api';
import { loadOperatorSecret } from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { BotStatusSummary } from './bot/BotStatusSummary';
import { loadBotWorkspace } from './bot/loadBotWorkspace';
import {
  defaultModerationAction,
  defaultModerationDraft,
  defaultModerationTest,
  defaultNukeDraft,
  type BotPageState,
  type ModerationDraft,
} from './bot/types';
import { CustomizablePanelLayout } from './components/CustomizablePanelLayout';
import type { PanelLayoutItem } from '../../lib/dashboardLayout';
import { loadDashboardConfig, saveDashboardConfig } from '../../lib/dashboardConfig';
import { Notice } from './ui';
import { BotCommandsPanel } from './bot/BotCommandsPanel';
import { BotCommunityPanel } from './bot/BotCommunityPanel';
import { BotAPIVariablesPanel } from './bot/BotAPIVariablesPanel';
import { BotModerationPanel } from './bot/BotModerationPanel';
import { BotTimersPanel } from './bot/BotTimersPanel';
import { useBotTimers } from './bot/useBotTimers';
import { useBotCommunity } from './bot/useBotCommunity';
import { useBotCommands } from './bot/useBotCommands';
import { useBotAPIVariables } from './bot/useBotAPIVariables';

export function BotWorkspace() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const operatorSecret = loadOperatorSecret();
  const [config, setConfig] = useState(() => loadDashboardConfig());
  const workspaceGeneration = useRef(0);
  const summaryRequest = useRef(0);
  const [state, setState] = useState<BotPageState>({ status: null, audit: null, moderation: null, moderationDryRun: null, loading: false, error: '' });
  const [moderationDraft, setModerationDraft] = useState<ModerationDraft>(defaultModerationDraft);
  const [moderationTest, setModerationTest] = useState(defaultModerationTest);
  const [moderationAction, setModerationAction] = useState(defaultModerationAction);
  const [moderationActionResult, setModerationActionResult] = useState('');
  const [nukeDraft, setNukeDraft] = useState(defaultNukeDraft);
  const [nukeResult, setNukeResult] = useState<BotNukeDryRunResponse | null>(null);
  const [nukeExecuteResult, setNukeExecuteResult] = useState<BotNukeExecuteResponse | null>(null);
  const [nukeRollbackResult, setNukeRollbackResult] = useState('');

  const timers = useBotTimers(apiBaseUrl, operatorSecret, refreshBotSummary);
  const community = useBotCommunity(apiBaseUrl, operatorSecret, refreshBotSummary);
  const commands = useBotCommands(apiBaseUrl, operatorSecret, refreshBotSummary);
  const apiVariables = useBotAPIVariables(apiBaseUrl, operatorSecret, refreshBotSummary);

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

  async function refreshModeration() {
    const moderation = await fetchBotModerationRules(apiBaseUrl, operatorSecret);
    setState((current) => ({ ...current, moderation }));
  }

  async function handleSaveModerationRule() {
    try { await saveBotModerationRule(moderationDraft, apiBaseUrl, operatorSecret); setModerationDraft(defaultModerationDraft); await refreshModeration(); }
    catch (error) { setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'moderation rule save failed' })); }
  }

  async function handleDeleteModerationRule(rule: BotModerationRule) {
    try { await deleteBotModerationRule(rule.id, apiBaseUrl, operatorSecret); await refreshModeration(); }
    catch (error) { setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'moderation rule delete failed' })); }
  }

  async function handleModerationDryRun() {
    try { const moderationDryRun = await dryRunBotModeration(moderationTest, apiBaseUrl, operatorSecret); setState((current) => ({ ...current, moderationDryRun, error: '' })); }
    catch (error) { setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'moderation dry-run failed' })); }
  }

  async function handleModerationAction() {
    try {
      const result = await executeBotModerationAction(moderationAction, apiBaseUrl, operatorSecret);
      setModerationActionResult(`${result.action} ${result.result}`);
      setState((current) => ({ ...current, error: '' }));
    } catch (error) {
      setModerationActionResult('');
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'moderation action failed' }));
    }
  }

  async function handleNukeDryRun() {
    try {
      const result = await dryRunBotNuke(nukeDraft, apiBaseUrl, operatorSecret);
      setNukeResult(result);
      setNukeExecuteResult(null);
      setNukeRollbackResult('');
      setNukeDraft((current) => ({ ...current, confirm: false }));
      setState((current) => ({ ...current, error: '' }));
    } catch (error) {
      setNukeResult(null);
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'nuke dry-run failed' }));
    }
  }

  async function handleNukeExecute() {
    try {
      const result = await executeBotNuke(nukeDraft, apiBaseUrl, operatorSecret);
      setNukeExecuteResult(result);
      setNukeRollbackResult('');
      setNukeDraft((current) => ({ ...current, confirm: false }));
      await refreshModeration();
      setState((current) => ({ ...current, error: '' }));
    } catch (error) {
      setNukeExecuteResult(null);
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'nuke execute failed' }));
    }
  }

  async function handleNukeRollback(rule: BotModerationRule) {
    try {
      const result = await rollbackBotNukeRadiation(rule.id, apiBaseUrl, operatorSecret);
      setNukeRollbackResult(`disabled ${result.rule.name}`);
      await refreshModeration();
      setState((current) => ({ ...current, error: '' }));
    } catch (error) {
      setNukeRollbackResult('');
      setState((current) => ({ ...current, error: error instanceof Error ? error.message : 'nuke rollback failed' }));
    }
  }

  return (
    <>
      {state.error ? (
        <Notice tone="bad" role="alert">
          {state.error}
        </Notice>
      ) : null}

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

      <BotModerationPanel
        rules={state.moderation}
        draft={moderationDraft}
        test={moderationTest}
        dryRun={state.moderationDryRun}
        actionDraft={moderationAction}
        actionResult={moderationActionResult}
        nukeDraft={nukeDraft}
        nukeResult={nukeResult}
        nukeExecuteResult={nukeExecuteResult}
        nukeRollbackResult={nukeRollbackResult}
        onDraftChange={setModerationDraft}
        onTestChange={setModerationTest}
        onActionChange={setModerationAction}
        onNukeDraftChange={setNukeDraft}
        onSave={handleSaveModerationRule}
        onDelete={handleDeleteModerationRule}
        onDryRun={handleModerationDryRun}
        onAction={handleModerationAction}
        onNukeDryRun={handleNukeDryRun}
        onNukeExecute={handleNukeExecute}
        onNukeRollback={handleNukeRollback}
      />
    </>
  );
}

