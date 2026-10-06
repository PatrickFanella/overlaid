import { useMemo, useState } from 'react';
import {
  fetchOBSInputs,
  fetchOBSSceneItems,
  fetchOBSScenes,
  refreshOBSSource,
  setOBSInputMute,
  setOBSScene,
  setOBSSceneItemEnabled,
  type OBSInput,
  type OBSScene,
  type OBSSceneItem,
} from '../../../lib/api';
import { Empty, Notice, Panel, SubPanel, Tag } from '../ui';

type OBSControlsProps = {
  apiBaseUrl: string;
  operatorSecret: string;
  isRunning: boolean;
  compact?: boolean;
  runAction: (label: string, action: () => Promise<void>) => void;
};

type InventoryState = {
  scenes: OBSScene[];
  inputs: OBSInput[];
  sceneItems: OBSSceneItem[];
  currentSceneName: string;
  selectedSceneName: string;
  loading: boolean;
  error: string;
};

const defaultState: InventoryState = {
  scenes: [],
  inputs: [],
  sceneItems: [],
  currentSceneName: '',
  selectedSceneName: '',
  loading: false,
  error: '',
};

export function OBSControls({ apiBaseUrl, operatorSecret, isRunning, compact = false, runAction }: OBSControlsProps) {
  const [state, setState] = useState<InventoryState>(defaultState);
  const actionDisabledReason = !operatorSecret ? 'Operator secret is missing. Add it in Settings before running OBS actions.' : isRunning ? 'Another operator action is running.' : '';

  async function refreshSceneItems(sceneName: string) {
    try {
      const items = await fetchOBSSceneItems(sceneName, apiBaseUrl, operatorSecret);
      setState((current) => ({
        ...current,
        selectedSceneName: sceneName,
        sceneItems: items,
      }));
    } catch {
      setState((current) => ({
        ...current,
        selectedSceneName: sceneName,
        sceneItems: [],
      }));
    }
  }

  async function loadInventory() {
    setState((current) => ({ ...current, loading: true, error: '' }));

    const [scenesResult, inputsResult] = await Promise.allSettled([fetchOBSScenes(apiBaseUrl, operatorSecret), fetchOBSInputs(apiBaseUrl, operatorSecret)]);

    const scenes = scenesResult.status === 'fulfilled' ? scenesResult.value.scenes : [];
    const currentSceneName = scenesResult.status === 'fulfilled' ? scenesResult.value.currentProgramSceneName : '';
    const selectedSceneName = currentSceneName || scenes[0]?.sceneName || '';
    setState({
      scenes,
      inputs: inputsResult.status === 'fulfilled' ? inputsResult.value : [],
      sceneItems: [],
      currentSceneName,
      selectedSceneName,
      loading: false,
      error:
        scenesResult.status === 'rejected' && inputsResult.status === 'rejected'
          ? 'Unable to load OBS inventory.'
          : scenesResult.status === 'rejected'
            ? 'Unable to load OBS scenes.'
            : inputsResult.status === 'rejected'
              ? 'Unable to load OBS inputs.'
              : '',
    });

    if (selectedSceneName) {
      await loadSceneItems(selectedSceneName);
    }
  }

  async function loadSceneItems(sceneName: string) {
    const itemsResult = await fetchOBSSceneItems(sceneName, apiBaseUrl, operatorSecret)
      .then((value) => ({ status: 'fulfilled' as const, value }))
      .catch((error: unknown) => ({ status: 'rejected' as const, error }));

    setState((current) => ({
      ...current,
      selectedSceneName: sceneName,
      sceneItems: itemsResult.status === 'fulfilled' ? itemsResult.value : [],
    }));
  }

  const selectedScene = useMemo(() => state.scenes.find((scene) => scene.sceneName === state.selectedSceneName), [state.scenes, state.selectedSceneName]);

  function chooseScene(sceneName: string) {
    runAction(`Set scene: ${sceneName}`, async () => {
      await setOBSScene(sceneName, apiBaseUrl, operatorSecret);
      setState((current) => ({ ...current, currentSceneName: sceneName, selectedSceneName: sceneName }));
      await refreshSceneItems(sceneName);
    });
  }

  function toggleInput(inputName: string, muted: boolean) {
    runAction(`${muted ? 'Mute' : 'Unmute'} input: ${inputName}`, async () => {
      await setOBSInputMute(inputName, muted, apiBaseUrl, operatorSecret);
      setState((current) => ({
        ...current,
        inputs: current.inputs.map((input) => (input.inputName === inputName ? { ...input } : input)),
      }));
    });
  }

  function toggleSceneItem(sceneItemId: number, enabled: boolean) {
    const sceneName = state.selectedSceneName || selectedScene?.sceneName || '';
    if (!sceneName) {
      return;
    }

    runAction(`${enabled ? 'Enable' : 'Disable'} item ${sceneItemId}`, async () => {
      await setOBSSceneItemEnabled(sceneName, sceneItemId, enabled, apiBaseUrl, operatorSecret);
      setState((current) => ({
        ...current,
        sceneItems: current.sceneItems.map((item) => (item.sceneItemId === sceneItemId ? { ...item, sceneItemEnabled: enabled } : item)),
      }));
    });
  }

  function refreshSource(inputName: string) {
    runAction(`Refresh source: ${inputName}`, async () => {
      await refreshOBSSource(inputName, apiBaseUrl, operatorSecret);
    });
  }

  return (
    <Panel
      kicker="Scene controls"
      title="OBS controls"
      description={compact ? undefined : 'Inventory loads only when you press refresh, so opening the page does not open and close OBS WebSocket connections.'}
      actions={
        <>
          <button className="desk-btn desk-btn--small" type="button" disabled={isRunning || state.loading} title={isRunning ? actionDisabledReason : undefined} onClick={() => void loadInventory()}>
            {state.loading ? 'Loading' : 'Refresh OBS'}
          </button>
          <Tag tone={state.currentSceneName ? 'ok' : 'neutral'} title="Program scene">
            {state.currentSceneName || 'not loaded'}
          </Tag>
        </>
      }
    >
      {actionDisabledReason ? <Notice tone="warn">{actionDisabledReason}</Notice> : null}
      {state.error ? <Notice tone="bad" role="alert">{state.error}</Notice> : null}

      <div className="grid gap-3 lg:grid-cols-2">
        <SubPanel title="Scenes">
          {state.loading && state.scenes.length === 0 ? <Empty>Loading scene inventory.</Empty> : null}
          {!state.loading && state.scenes.length === 0 ? <Empty>No scenes loaded.</Empty> : null}
          {state.scenes.length > 0 ? (
            <div className="grid gap-1">
              {state.scenes.map((scene) => (
                <button
                  key={scene.sceneName}
                  className={`desk-btn justify-between ${scene.sceneName === state.currentSceneName ? 'desk-btn--active' : ''}`}
                  type="button"
                  aria-pressed={scene.sceneName === state.currentSceneName}
                  disabled={!!actionDisabledReason}
                  title={actionDisabledReason || undefined}
                  onClick={() => chooseScene(scene.sceneName)}
                >
                  <span className="truncate">{scene.sceneName}</span>
                  <span className="mono">{scene.sceneIndex + 1}</span>
                </button>
              ))}
            </div>
          ) : null}
        </SubPanel>

        <SubPanel title="Inputs">
          {state.inputs.length === 0 ? (
            <Empty>No inputs loaded.</Empty>
          ) : (
            <ul className="desk-rows">
              {state.inputs.map((input) => (
                <li key={input.inputName} className="desk-row">
                  <div className="desk-row__line">
                    <div className="min-w-0">
                      <p className="desk-row__title">{input.inputName}</p>
                      <span className="desk-row__meta mono">{input.inputKind || 'input'}</span>
                    </div>
                    <button className="desk-btn desk-btn--small" type="button" disabled={!!actionDisabledReason} title={actionDisabledReason || undefined} onClick={() => toggleInput(input.inputName, true)}>
                      Mute
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </SubPanel>
      </div>

      <SubPanel title="Current scene items">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="desk-field__hint">{state.selectedSceneName ? `Editing ${state.selectedSceneName}` : 'Select a scene to inspect its source items.'}</p>
          {selectedScene ? <span className="mono">#{selectedScene.sceneIndex + 1}</span> : null}
        </div>

        {state.sceneItems.length === 0 ? (
          <Empty>No scene items loaded.</Empty>
        ) : (
          <ul className="desk-rows">
            {state.sceneItems.map((item) => (
              <li key={item.sceneItemId} className={`desk-row ${item.sceneItemEnabled ? '' : 'desk-row--warn'}`}>
                <div className="desk-row__line">
                  <div className="min-w-0">
                    <p className="desk-row__title">{item.sourceName}</p>
                    <span className="desk-row__meta">{item.sceneItemEnabled ? 'enabled' : 'hidden'}</span>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <button className="desk-btn desk-btn--small" type="button" disabled={!!actionDisabledReason} title={actionDisabledReason || undefined} onClick={() => refreshSource(item.sourceName)}>
                      Refresh
                    </button>
                    <button className="desk-btn desk-btn--small" type="button" disabled={!!actionDisabledReason} title={actionDisabledReason || undefined} onClick={() => toggleSceneItem(item.sceneItemId, !item.sceneItemEnabled)}>
                      {item.sceneItemEnabled ? 'Disable' : 'Enable'}
                    </button>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </SubPanel>
    </Panel>
  );
}
