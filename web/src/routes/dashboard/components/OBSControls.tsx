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
  const actionDisabledReason = !operatorSecret ? 'Operator secret is missing. Open Config and save the secret before running OBS actions.' : isRunning ? 'Another operator action is running.' : '';

  async function refreshSceneItems(sceneName: string) {
    try {
      const items = await fetchOBSSceneItems(sceneName, apiBaseUrl);
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

    const [scenesResult, inputsResult] = await Promise.allSettled([fetchOBSScenes(apiBaseUrl), fetchOBSInputs(apiBaseUrl)]);

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
    const itemsResult = await fetchOBSSceneItems(sceneName, apiBaseUrl)
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
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
        <div>
          <p className="hud-label">Scene controls</p>
          <h2 className="hud-section-title">OBS controls</h2>
          {!compact ? <p className="hud-copy mt-1 text-xs text-zinc-300">Click refresh to load OBS inventory. This avoids WebSocket connect/disconnect spam on page load.</p> : null}
          {actionDisabledReason ? <p className="mt-2 text-xs text-amber-200">{actionDisabledReason}</p> : null}
        </div>
        <div className="flex gap-2">
          <button className="hud-chip hud-chip--square" type="button" disabled={isRunning || state.loading} title={isRunning ? actionDisabledReason : undefined} onClick={() => void loadInventory()}>{state.loading ? 'loading' : 'refresh OBS'}</button>
          <span className="hud-chip hud-chip--square">{state.currentSceneName || 'not loaded'}</span>
        </div>
      </div>

      <div className="mt-3 grid gap-3 lg:grid-cols-[1fr_1fr]">
        <article className="hud-panel hud-corners hud-panel--flat hud-compact-card">
          <p className="hud-kicker">Scenes</p>
          <div className="mt-4 grid gap-2">
            {state.loading && state.scenes.length === 0 ? <p className="text-sm text-zinc-400">Loading scene inventory…</p> : null}
            {state.error ? <p className="rounded-2xl border border-amber-300/20 bg-amber-300/10 px-3 py-2 text-sm text-amber-100">{state.error}</p> : null}
            {state.scenes.map((scene) => (
              <button
                key={scene.sceneName}
                className={`hud-chip justify-between rounded-2xl px-4 py-3 text-left ${scene.sceneName === state.currentSceneName ? 'border-emerald-300/40 text-emerald-100' : ''}`}
                type="button"
                disabled={!!actionDisabledReason}
                title={actionDisabledReason || undefined}
                onClick={() => chooseScene(scene.sceneName)}
              >
                <span>{scene.sceneName}</span>
                <span>{scene.sceneIndex + 1}</span>
              </button>
            ))}
          </div>
        </article>

        <article className="hud-panel hud-corners hud-panel--flat hud-compact-card">
          <p className="hud-kicker">Inputs</p>
          <div className="mt-4 grid gap-2">
            {state.inputs.length === 0 ? <p className="text-sm text-zinc-400">No input inventory loaded.</p> : null}
            {state.inputs.map((input) => (
              <div key={input.inputName} className="flex items-center justify-between gap-3 rounded-2xl border border-white/10 bg-black/20 px-3 py-3">
                <div>
                  <p className="text-sm font-semibold text-white">{input.inputName}</p>
                  <p className="text-xs uppercase tracking-[0.24em] text-zinc-400">{input.inputKind || 'input'}</p>
                </div>
                <button className="hud-chip" type="button" disabled={!!actionDisabledReason} title={actionDisabledReason || undefined} onClick={() => toggleInput(input.inputName, true)}>
                  Mute
                </button>
              </div>
            ))}
          </div>
        </article>
      </div>

      <article className="mt-3 hud-panel hud-corners hud-panel--flat hud-compact-card">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <p className="hud-kicker">Current scene items</p>
            <p className="mt-2 text-sm text-zinc-300">{state.selectedSceneName ? `Editing ${state.selectedSceneName}` : 'Select a scene to inspect source items.'}</p>
          </div>
          {selectedScene ? <span className="hud-chip">#{selectedScene.sceneIndex + 1}</span> : null}
        </div>

        <div className="mt-4 grid gap-2">
          {state.sceneItems.length === 0 ? <p className="text-sm text-zinc-400">No scene items loaded.</p> : null}
          {state.sceneItems.map((item) => (
            <div key={item.sceneItemId} className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-white/10 bg-black/20 px-3 py-3">
              <div>
                <p className="text-sm font-semibold text-white">{item.sourceName}</p>
                <p className="text-xs uppercase tracking-[0.24em] text-zinc-400">{item.sceneItemEnabled ? 'enabled' : 'hidden'}</p>
              </div>
              <div className="flex flex-wrap gap-2">
                <button className="hud-chip" type="button" disabled={!!actionDisabledReason} title={actionDisabledReason || undefined} onClick={() => refreshSource(item.sourceName)}>
                  Refresh
                </button>
                <button className="hud-chip" type="button" disabled={!!actionDisabledReason} title={actionDisabledReason || undefined} onClick={() => toggleSceneItem(item.sceneItemId, !item.sceneItemEnabled)}>
                  {item.sceneItemEnabled ? 'Disable' : 'Enable'}
                </button>
              </div>
            </div>
          ))}
        </div>
      </article>
    </section>
  );
}
