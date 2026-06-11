import { useEffect, useMemo, useState } from 'react';
import {
  fetchOBSInputs,
  fetchOBSSceneItems,
  fetchOBSScenes,
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
  loading: true,
  error: '',
};

export function OBSControls({ apiBaseUrl, operatorSecret, isRunning, runAction }: OBSControlsProps) {
  const [state, setState] = useState<InventoryState>(defaultState);

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

  useEffect(() => {
    let cancelled = false;

    async function load() {
      setState((current) => ({ ...current, loading: true, error: '' }));

      const [scenesResult, inputsResult] = await Promise.allSettled([fetchOBSScenes(apiBaseUrl), fetchOBSInputs(apiBaseUrl)]);
      if (cancelled) {
        return;
      }

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
        await loadSceneItems(selectedSceneName, cancelled);
      }
    }

    async function loadSceneItems(sceneName: string, wasCancelled = false) {
      const itemsResult = await fetchOBSSceneItems(sceneName, apiBaseUrl)
        .then((value) => ({ status: 'fulfilled' as const, value }))
        .catch((error: unknown) => ({ status: 'rejected' as const, error }));
      if (cancelled || wasCancelled) {
        return;
      }

      setState((current) => ({
        ...current,
        selectedSceneName: sceneName,
        sceneItems: itemsResult.status === 'fulfilled' ? itemsResult.value : [],
      }));
    }

    void load();

    return () => {
      cancelled = true;
    };
  }, [apiBaseUrl]);

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

  return (
    <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
      <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
        <div>
          <p className="hud-label">Scene controls</p>
          <h2 className="mt-3 text-2xl font-black text-white">OBS controls</h2>
          <p className="hud-copy mt-2 text-sm text-zinc-300">Select scenes, mute inputs, and toggle source items with the operator secret already loaded for this session.</p>
        </div>
        <span className="hud-chip">{state.currentSceneName || 'unknown scene'}</span>
      </div>

      <div className="mt-5 grid gap-4 lg:grid-cols-[1fr_1fr]">
        <article className="hud-panel hud-corners rounded-[1.2rem] p-4">
          <p className="hud-kicker">Scenes</p>
          <div className="mt-4 grid gap-2">
            {state.loading && state.scenes.length === 0 ? <p className="text-sm text-zinc-400">Loading scene inventory…</p> : null}
            {state.error ? <p className="rounded-2xl border border-amber-300/20 bg-amber-300/10 px-3 py-2 text-sm text-amber-100">{state.error}</p> : null}
            {state.scenes.map((scene) => (
              <button
                key={scene.sceneName}
                className={`hud-chip justify-between rounded-2xl px-4 py-3 text-left ${scene.sceneName === state.currentSceneName ? 'border-emerald-300/40 text-emerald-100' : ''}`}
                type="button"
                disabled={isRunning}
                onClick={() => chooseScene(scene.sceneName)}
              >
                <span>{scene.sceneName}</span>
                <span>{scene.sceneIndex + 1}</span>
              </button>
            ))}
          </div>
        </article>

        <article className="hud-panel hud-corners rounded-[1.2rem] p-4">
          <p className="hud-kicker">Inputs</p>
          <div className="mt-4 grid gap-2">
            {state.inputs.length === 0 ? <p className="text-sm text-zinc-400">No input inventory loaded.</p> : null}
            {state.inputs.map((input) => (
              <div key={input.inputName} className="flex items-center justify-between gap-3 rounded-2xl border border-white/10 bg-black/20 px-3 py-3">
                <div>
                  <p className="text-sm font-semibold text-white">{input.inputName}</p>
                  <p className="text-xs uppercase tracking-[0.24em] text-zinc-400">{input.inputKind || 'input'}</p>
                </div>
                <button className="hud-chip" type="button" disabled={isRunning} onClick={() => toggleInput(input.inputName, true)}>
                  Mute
                </button>
              </div>
            ))}
          </div>
        </article>
      </div>

      <article className="mt-4 hud-panel hud-corners rounded-[1.2rem] p-4">
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
              <button className="hud-chip" type="button" disabled={isRunning} onClick={() => toggleSceneItem(item.sceneItemId, !item.sceneItemEnabled)}>
                {item.sceneItemEnabled ? 'Disable' : 'Enable'}
              </button>
            </div>
          ))}
        </div>
      </article>
    </section>
  );
}
