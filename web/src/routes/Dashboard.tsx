import { FormEvent, useEffect, useState } from 'react';
import {
  apiUrl,
  clearOverlays,
  fetchTwitchLoginURL,
  fetchOBSScenes,
  fetchOBSStatus,
  fetchTwitchStatus,
  sendTestAlert,
  sendWebhook,
  setBRBStatus,
  setOBSInputMute,
  setOBSScene,
  setOBSSceneItemEnabled,
  type AlertPayload,
  type BRBPayload,
  type OBSSceneList,
  type OBSStatus,
  type TwitchStatus,
} from '../lib/api';
import { getOverlayApiBaseUrl } from '../lib/eventStream';

const overlayRoutes = [
  ['/overlay/alerts', 'Alert feed', 'Live alert browser source'],
  ['/overlay/chat', 'Twitch chat', 'Live chat browser source'],
  ['/overlay/goals', 'Goal rail', 'Progress widget browser source'],
  ['/overlay/now-playing', 'Now playing', 'Music and media browser source'],
  ['/overlay/ticker', 'Ticker rail', 'Lower-third event ticker'],
  ['/overlay/brb', 'BRB overlay', 'Intermission scene control'],
  ['/overlay/debug', 'Event inspector', 'SSE event stream debugger'],
] as const;

const defaultAlert: AlertPayload = {
  title: 'Dashboard test',
  message: 'Overlay command center is live',
  severity: 'success',
};

const defaultBRB: BRBPayload = {
  active: true,
  title: 'Be right back',
  message: 'Short intermission. Stream resumes soon.',
  eta: '5 min',
  items: ['Hydrate', 'Stretch', 'Chat stays live'],
};

type ActionState = 'idle' | 'running' | 'success' | 'error';

export function DashboardPage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const [status, setStatus] = useState<TwitchStatus | null>(null);
  const [obsStatus, setOBSStatus] = useState<OBSStatus | null>(null);
  const [obsScenes, setOBSScenes] = useState<OBSSceneList | null>(null);
  const [statusError, setStatusError] = useState('');
  const [obsError, setOBSError] = useState('');
  const [actionState, setActionState] = useState<ActionState>('idle');
  const [actionMessage, setActionMessage] = useState('');
  const [alert, setAlert] = useState(defaultAlert);
  const [brb, setBRB] = useState(defaultBRB);
  const [brbItemsText, setBRBItemsText] = useState(defaultBRB.items.join('\n'));
  const [operatorSecret, setOperatorSecret] = useState('');
  const [webhookSource, setWebhookSource] = useState('n8n');
  const [webhookSecret, setWebhookSecret] = useState('');
  const [selectedScene, setSelectedScene] = useState('');
  const [inputName, setInputName] = useState('Mic/Aux');
  const [sceneItemID, setSceneItemID] = useState('1');
  const [clearArmed, setClearArmed] = useState(false);

  const isRunning = actionState === 'running';
  const twitchSummary = status?.authenticated ? `connected · ${status.streamerLogin || status.channelTarget || 'ready'}` : status?.authError ? 'auth error' : status?.configured === false ? 'not configured' : 'offline';
  const obsSummary = obsStatus?.authenticated ? `connected · ${obsScenes?.currentProgramSceneName || obsStatus.obsStudioVersion || 'ready'}` : obsStatus?.error ? 'auth error' : obsStatus?.configured === false ? 'not configured' : 'offline';
  const apiSummary = apiBaseUrl;
  const actionSummary = clearArmed ? 'confirm clear armed' : actionState === 'running' ? actionMessage || 'running' : actionState === 'success' ? actionMessage || 'idle' : actionState === 'error' ? actionMessage || 'error' : 'idle';

  useEffect(() => {
    let cancelled = false;
    fetchTwitchStatus(apiBaseUrl)
      .then((nextStatus) => {
        if (!cancelled) {
          setStatus(nextStatus);
          setStatusError('');
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setStatusError(error instanceof Error ? error.message : 'Unable to load Twitch status');
        }
      });

    return () => {
      cancelled = true;
    };
  }, [apiBaseUrl]);

  useEffect(() => {
    let cancelled = false;
    Promise.all([fetchOBSStatus(apiBaseUrl), fetchOBSScenes(apiBaseUrl)])
      .then(([nextStatus, nextScenes]) => {
        if (!cancelled) {
          setOBSStatus(nextStatus);
          setOBSScenes(nextScenes);
          setSelectedScene((current) => current || nextScenes.currentProgramSceneName || nextScenes.scenes[0]?.sceneName || '');
          setOBSError('');
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setOBSError(error instanceof Error ? error.message : 'Unable to load OBS status');
        }
      });

    return () => {
      cancelled = true;
    };
  }, [apiBaseUrl]);

  async function runAction(label: string, action: () => Promise<void>) {
    setClearArmed(false);
    setActionState('running');
    setActionMessage(label);
    try {
      await action();
      setActionState('success');
      setActionMessage(`${label} complete`);
    } catch (error) {
      setActionState('error');
      setActionMessage(error instanceof Error ? error.message : `${label} failed`);
    }
  }

  function submitTestAlert(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (isRunning) {
      return;
    }
    void runAction('Send test alert', () => sendTestAlert(alert, apiBaseUrl, operatorSecret));
  }

  function submitWebhook(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (isRunning) {
      return;
    }
    void runAction(`Send ${webhookSource} webhook`, () => sendWebhook(webhookSource, alert, apiBaseUrl, webhookSecret));
  }

  async function startTwitchLogin() {
    if (isRunning) {
      return;
    }
    const { authorizeUrl } = await fetchTwitchLoginURL(apiBaseUrl, operatorSecret);
    window.location.href = authorizeUrl;
  }

  function submitBRB(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (isRunning) {
      return;
    }
    const nextBRB = { ...brb, active: true, items: splitLines(brbItemsText) };
    setBRB(nextBRB);
    void runAction('Update BRB overlay', () => setBRBStatus(nextBRB, apiBaseUrl, operatorSecret));
  }

  function hideBRB() {
    if (isRunning) {
      return;
    }
    const nextBRB = { ...brb, active: false, items: splitLines(brbItemsText) };
    setBRB(nextBRB);
    void runAction('Hide BRB overlay', () => setBRBStatus(nextBRB, apiBaseUrl, operatorSecret));
  }

  function armClearOverlays() {
    if (isRunning) {
      return;
    }

    if (!clearArmed) {
      setActionState('idle');
      setActionMessage('Clear overlays armed. Click again to confirm.');
      setClearArmed(true);
      return;
    }

    void runAction('Clear overlays', () => clearOverlays(apiBaseUrl, operatorSecret));
  }

  return (
    <main className="hud-shell hud-scanlines min-h-screen bg-[radial-gradient(circle_at_top_left,rgba(103,232,249,0.14),transparent_28%),radial-gradient(circle_at_top_right,rgba(244,114,182,0.12),transparent_34%),linear-gradient(135deg,#020617,#0b1020_54%,#020305)] px-6 py-8 text-zinc-100">
      <section className="mx-auto flex max-w-7xl flex-col gap-6">
        <header className="hud-panel hud-corners rounded-[1.8rem] p-6 xl:p-7">
          <div className="flex flex-col gap-6 xl:flex-row xl:items-end xl:justify-between">
            <div className="max-w-3xl">
              <p className="hud-label">over-laid command center</p>
              <h1 className="hud-title mt-4">Dashboard</h1>
              <p className="hud-copy mt-3 max-w-2xl text-sm sm:text-base">Control overlays, check Twitch auth, and fire test events without leaving the browser.</p>
            </div>

            <div className="hud-panel hud-corners hud-accent-cyan w-full rounded-[1.35rem] p-5 xl:max-w-[28rem]">
              <div className="flex items-center justify-between gap-3">
                <p className="hud-label">Operator secret</p>
                <span className="hud-chip">API ready</span>
              </div>
              <p className="mt-3 text-sm text-zinc-300">Used for Twitch login URL requests, OBS commands, BRB control, and overlay clears.</p>
              <label className="mt-4 grid gap-2 text-sm text-zinc-300">
                <span className="hud-kicker">Secret header</span>
                <input className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-fuchsia-300/30 focus:ring-4" type="password" value={operatorSecret} onChange={(event) => setOperatorSecret(event.target.value)} placeholder="optional unless API requires it" />
              </label>
              <div className="mt-4 flex flex-wrap gap-2 text-xs text-zinc-400">
                <span className="hud-badge">{apiBaseUrl}</span>
                <span className="hud-badge">X-Overlay-Operator-Secret</span>
              </div>
            </div>
          </div>
        </header>

        <section className="hud-panel hud-corners hud-accent-cyan rounded-[1.6rem] p-5">
          <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
            <div>
              <p className="hud-label">System status</p>
              <p className="hud-copy mt-2 text-sm">Live summaries for Twitch, OBS, API reachability, and action state.</p>
            </div>
            <span className="hud-chip">
              <span className="hud-led" />
              {actionSummary}
            </span>
          </div>

          <div className="mt-5 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <div className="hud-panel hud-corners hud-accent-fuchsia rounded-[1.25rem] p-4">
              <p className="hud-label">Twitch</p>
              <p className="mt-3 text-lg font-semibold text-white">{twitchSummary}</p>
              <p className="mt-2 text-sm text-zinc-300">{statusError || formatDate(status?.tokenExpiresAt)}</p>
            </div>
            <div className="hud-panel hud-corners hud-accent-emerald rounded-[1.25rem] p-4">
              <p className="hud-label">OBS</p>
              <p className="mt-3 text-lg font-semibold text-white">{obsSummary}</p>
              <p className="mt-2 text-sm text-zinc-300">{obsError || obsStatus?.obsWebSocketVersion || 'awaiting connection'}</p>
            </div>
            <div className="hud-panel hud-corners hud-accent-cyan rounded-[1.25rem] p-4">
              <p className="hud-label">API</p>
              <p className="mt-3 break-all text-lg font-semibold text-white">{apiSummary}</p>
              <p className="mt-2 text-sm text-zinc-300">{apiUrl('/api/auth/twitch/status', apiBaseUrl)}</p>
            </div>
            <div className="hud-panel hud-corners hud-accent-amber rounded-[1.25rem] p-4">
              <p className="hud-label">Action</p>
              <p className="mt-3 text-lg font-semibold text-white">{actionState}</p>
              <p className="mt-2 text-sm text-zinc-300">{clearArmed ? 'Clear overlays armed' : actionMessage || 'Waiting for commands'}</p>
            </div>
          </div>
        </section>

        <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
          <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
            <div>
              <p className="hud-label">Overlay monitor</p>
              <p className="hud-copy mt-2 text-sm">Text-only route cards for every overlay browser source.</p>
            </div>
            <span className="hud-chip">open routes</span>
          </div>

          <div className="mt-5 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {overlayRoutes.map(([href, title, description]) => (
              <article key={href} className="hud-panel hud-corners hud-dashboard-card rounded-[1.35rem] p-4">
                <p className="hud-kicker">{href}</p>
                <h2 className="mt-2 text-lg font-semibold text-white">{title}</h2>
                <p className="mt-2 text-sm leading-6 text-zinc-300">{description}</p>
                <div className="mt-4 flex items-center justify-between gap-3">
                  <span className="hud-badge">browser source</span>
                  <a className="hud-chip" href={href}>
                    Open link
                  </a>
                </div>
              </article>
            ))}
          </div>
        </section>

        <div className="grid gap-6 xl:grid-cols-[1.05fr_0.95fr]">
          <section className="hud-panel hud-corners rounded-[1.6rem] p-6">
            <div className="flex items-start justify-between gap-4">
              <div>
                <p className="hud-label">Twitch</p>
                <h2 className="mt-2 text-2xl font-semibold text-white">Auth status</h2>
              </div>
              <StatusPill active={Boolean(status?.authenticated)} label={status?.authenticated ? 'authenticated' : 'offline'} />
            </div>

            {statusError ? <p className="mt-4 rounded-2xl border border-red-400/30 bg-red-500/10 px-4 py-3 text-sm text-red-100">{statusError}</p> : null}

            <dl className="mt-5 grid gap-3 text-sm sm:grid-cols-2">
              <StatusField label="Configured" value={status ? String(status.configured) : 'loading'} />
              <StatusField label="Streamer" value={status?.streamerLogin || status?.channelTarget || 'unknown'} />
              <StatusField label="User ID" value={status?.streamerUserId || 'not loaded'} />
              <StatusField label="Token expires" value={formatDate(status?.tokenExpiresAt)} />
            </dl>

            {status?.authError ? <p className="mt-4 rounded-2xl border border-amber-300/30 bg-amber-400/10 px-4 py-3 text-sm text-amber-100">{status.authError}</p> : null}

            <div className="mt-5 flex flex-wrap gap-3">
              <button className="rounded-full border border-fuchsia-300/40 bg-fuchsia-300/10 px-4 py-2 text-sm font-medium text-fuchsia-100 transition hover:bg-fuchsia-300/20 disabled:cursor-not-allowed disabled:border-white/10 disabled:bg-white/5 disabled:text-zinc-400" type="button" disabled={isRunning} onClick={() => void runAction('Start Twitch login', startTwitchLogin)}>
                Twitch login
              </button>
              <a className="rounded-full border border-white/15 bg-white/5 px-4 py-2 text-sm text-zinc-100 transition hover:bg-white/10" href={apiUrl('/api/auth/twitch/status', apiBaseUrl)}>
                Status JSON
              </a>
            </div>
          </section>

          <section className="hud-panel hud-corners rounded-[1.6rem] p-6">
            <p className="hud-label">Overlay controls</p>
            <h2 className="mt-2 text-2xl font-semibold text-white">Fire events</h2>
            <form className="mt-5 grid gap-4" onSubmit={submitTestAlert}>
              <TextInput label="Title" value={alert.title} onChange={(title) => setAlert((current) => ({ ...current, title }))} />
              <TextInput label="Message" value={alert.message} onChange={(message) => setAlert((current) => ({ ...current, message }))} />
              <label className="grid gap-2 text-sm text-zinc-300">
                <span className="hud-kicker">Severity</span>
                <select className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-fuchsia-300/30 focus:ring-4" value={alert.severity} onChange={(event) => setAlert((current) => ({ ...current, severity: event.target.value as AlertPayload['severity'] }))}>
                  <option value="info">info</option>
                  <option value="success">success</option>
                  <option value="warning">warning</option>
                  <option value="error">error</option>
                </select>
              </label>
              <div className="flex flex-wrap gap-3">
                <button className="rounded-full bg-fuchsia-300 px-5 py-2 text-sm font-semibold text-zinc-950 transition hover:bg-fuchsia-200 disabled:cursor-not-allowed disabled:bg-zinc-500 disabled:text-zinc-200" type="submit" disabled={isRunning}>
                  Send test alert
                </button>
                <button className={`rounded-full px-5 py-2 text-sm font-semibold transition disabled:cursor-not-allowed disabled:border-white/10 disabled:bg-white/5 disabled:text-zinc-400 ${clearArmed ? 'border-rose-300/45 bg-rose-300/15 text-rose-100 hover:bg-rose-300/25' : 'border-white/15 bg-white/5 text-zinc-100 hover:bg-white/10'}`} type="button" disabled={isRunning} onClick={armClearOverlays}>
                  {clearArmed ? 'Confirm clear' : 'Clear overlays'}
                </button>
              </div>
              <p className="text-sm text-zinc-400">Click once to arm, then click <span className="font-semibold text-rose-100">Confirm clear</span> to purge every live overlay. Arming resets after any other action.</p>
            </form>

            <form className="mt-5 flex flex-col gap-3 border-t border-white/10 pt-5 sm:flex-row" onSubmit={submitWebhook}>
              <label className="grid flex-1 gap-2 text-sm text-zinc-300">
                <span className="hud-kicker">Webhook source</span>
                <input className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4" value={webhookSource} onChange={(event) => setWebhookSource(event.target.value)} />
              </label>
              <label className="grid flex-1 gap-2 text-sm text-zinc-300">
                <span className="hud-kicker">Webhook secret</span>
                <input className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4" type="password" value={webhookSecret} onChange={(event) => setWebhookSecret(event.target.value)} placeholder="optional" />
              </label>
              <button className="self-end rounded-full border border-cyan-200/40 bg-cyan-200/10 px-5 py-3 text-sm font-medium text-cyan-100 transition hover:bg-cyan-200/20 disabled:cursor-not-allowed disabled:border-white/10 disabled:bg-white/5 disabled:text-zinc-400" type="submit" disabled={isRunning}>
                Send webhook
              </button>
            </form>

            {actionMessage ? <p className={`mt-4 rounded-2xl border px-4 py-3 text-sm ${actionClassName(actionState, clearArmed)}`} aria-live="polite">{actionMessage}</p> : null}
          </section>
        </div>

        <section className="hud-panel hud-corners rounded-[1.6rem] p-6">
          <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
            <div>
              <p className="hud-label">OBS</p>
              <h2 className="mt-2 text-2xl font-semibold text-white">Scene controls</h2>
              <p className="hud-copy mt-2 text-sm">Switch scenes, mute inputs, or toggle a scene item through OBS WebSocket.</p>
            </div>
            <StatusPill active={Boolean(obsStatus?.authenticated)} label={obsStatus?.authenticated ? 'connected' : 'offline'} />
          </div>

          {obsError ? <p className="mt-4 rounded-2xl border border-amber-300/30 bg-amber-400/10 px-4 py-3 text-sm text-amber-100">{obsError}</p> : null}

          <dl className="mt-5 grid gap-3 text-sm md:grid-cols-3">
            <StatusField label="URL" value={obsStatus?.url || 'not configured'} />
            <StatusField label="OBS" value={obsStatus?.obsStudioVersion || 'not connected'} />
            <StatusField label="Current scene" value={obsScenes?.currentProgramSceneName || 'not loaded'} />
          </dl>

          <div className="mt-5 grid gap-4 lg:grid-cols-3">
            <label className="grid gap-2 text-sm text-zinc-300">
              <span className="hud-kicker">Scene</span>
              <select className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-emerald-300/30 focus:ring-4" value={selectedScene} onChange={(event) => setSelectedScene(event.target.value)}>
                {(obsScenes?.scenes || []).map((scene) => (
                  <option key={scene.sceneName} value={scene.sceneName}>
                    {scene.sceneName}
                  </option>
                ))}
              </select>
              <button className="rounded-full bg-emerald-300 px-5 py-2 text-sm font-semibold text-zinc-950 transition hover:bg-emerald-200 disabled:cursor-not-allowed disabled:bg-zinc-500 disabled:text-zinc-200" type="button" disabled={isRunning} onClick={() => void runAction('Switch OBS scene', () => setOBSScene(selectedScene, apiBaseUrl, operatorSecret))}>
                Switch scene
              </button>
            </label>

            <label className="grid gap-2 text-sm text-zinc-300">
              <span className="hud-kicker">Input name</span>
              <input className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-emerald-300/30 focus:ring-4" value={inputName} onChange={(event) => setInputName(event.target.value)} />
              <div className="flex gap-2">
                <button className="flex-1 rounded-full border border-white/15 bg-white/5 px-4 py-2 text-sm text-zinc-100 transition hover:bg-white/10 disabled:cursor-not-allowed disabled:border-white/10 disabled:bg-white/5 disabled:text-zinc-400" type="button" disabled={isRunning} onClick={() => void runAction('Mute OBS input', () => setOBSInputMute(inputName, true, apiBaseUrl, operatorSecret))}>
                  Mute
                </button>
                <button className="flex-1 rounded-full border border-white/15 bg-white/5 px-4 py-2 text-sm text-zinc-100 transition hover:bg-white/10 disabled:cursor-not-allowed disabled:border-white/10 disabled:bg-white/5 disabled:text-zinc-400" type="button" disabled={isRunning} onClick={() => void runAction('Unmute OBS input', () => setOBSInputMute(inputName, false, apiBaseUrl, operatorSecret))}>
                  Unmute
                </button>
              </div>
            </label>

            <label className="grid gap-2 text-sm text-zinc-300">
              <span className="hud-kicker">Scene item ID</span>
              <input className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-emerald-300/30 focus:ring-4" inputMode="numeric" value={sceneItemID} onChange={(event) => setSceneItemID(event.target.value)} />
              <div className="flex gap-2">
                <button className="flex-1 rounded-full border border-white/15 bg-white/5 px-4 py-2 text-sm text-zinc-100 transition hover:bg-white/10 disabled:cursor-not-allowed disabled:border-white/10 disabled:bg-white/5 disabled:text-zinc-400" type="button" disabled={isRunning} onClick={() => void runAction('Show OBS scene item', () => setOBSSceneItemEnabled(selectedScene, Number(sceneItemID), true, apiBaseUrl, operatorSecret))}>
                  Show
                </button>
                <button className="flex-1 rounded-full border border-white/15 bg-white/5 px-4 py-2 text-sm text-zinc-100 transition hover:bg-white/10 disabled:cursor-not-allowed disabled:border-white/10 disabled:bg-white/5 disabled:text-zinc-400" type="button" disabled={isRunning} onClick={() => void runAction('Hide OBS scene item', () => setOBSSceneItemEnabled(selectedScene, Number(sceneItemID), false, apiBaseUrl, operatorSecret))}>
                  Hide
                </button>
              </div>
            </label>
          </div>
        </section>

        <section className="hud-panel hud-corners rounded-[1.6rem] p-6">
          <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
            <div>
              <p className="hud-label">Intermission</p>
              <h2 className="mt-2 text-2xl font-semibold text-white">BRB overlay</h2>
              <p className="hud-copy mt-2 text-sm">Push a live intermission state to `/overlay/brb`, or clear it back to standby.</p>
            </div>
            <StatusPill active={brb.active} label={brb.active ? 'armed' : 'standby'} />
          </div>

          <form className="mt-5 grid gap-4 lg:grid-cols-[1fr_1fr_0.7fr]" onSubmit={submitBRB}>
            <div className="grid gap-4">
              <TextInput label="BRB title" value={brb.title} onChange={(title) => setBRB((current) => ({ ...current, title }))} />
              <TextInput label="Message" value={brb.message} onChange={(message) => setBRB((current) => ({ ...current, message }))} />
              <TextInput label="Return ETA" value={brb.eta} onChange={(eta) => setBRB((current) => ({ ...current, eta }))} />
            </div>
            <label className="grid gap-2 text-sm text-zinc-300">
              <span className="hud-kicker">Notes, one per line</span>
              <textarea className="min-h-44 rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-rose-300/30 focus:ring-4" value={brbItemsText} onChange={(event) => setBRBItemsText(event.target.value)} />
            </label>
            <div className="grid content-end gap-3">
              <button className="rounded-full bg-rose-300 px-5 py-3 text-sm font-semibold text-zinc-950 transition hover:bg-rose-200 disabled:cursor-not-allowed disabled:bg-zinc-500 disabled:text-zinc-200" type="submit" disabled={isRunning}>
                Show BRB
              </button>
              <button className="rounded-full border border-white/15 bg-white/5 px-5 py-3 text-sm text-zinc-100 transition hover:bg-white/10 disabled:cursor-not-allowed disabled:border-white/10 disabled:bg-white/5 disabled:text-zinc-400" type="button" disabled={isRunning} onClick={hideBRB}>
                Hide BRB
              </button>
              <a className="rounded-full border border-rose-200/40 bg-rose-200/10 px-5 py-3 text-center text-sm font-medium text-rose-100 transition hover:bg-rose-200/20" href="/overlay/brb">
                Open BRB route
              </a>
            </div>
          </form>
        </section>
      </section>
    </main>
  );
}

function StatusPill({ active, label }: { active: boolean; label: string }) {
  return <span className={`rounded-full border px-3 py-1 text-xs uppercase tracking-[0.2em] ${active ? 'border-emerald-300/40 bg-emerald-300/10 text-emerald-100' : 'border-zinc-400/30 bg-zinc-400/10 text-zinc-300'}`}>{label}</span>;
}

function StatusField({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-2xl border border-white/10 bg-white/[0.04] p-4">
      <dt className="text-xs uppercase tracking-[0.25em] text-zinc-500">{label}</dt>
      <dd className="mt-2 break-words font-mono text-sm text-zinc-100">{value}</dd>
    </div>
  );
}

function TextInput({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label className="grid gap-2 text-sm text-zinc-300">
      <span className="hud-kicker">{label}</span>
      <input className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-fuchsia-300/30 focus:ring-4" value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}

function formatDate(value?: string) {
  if (!value) {
    return 'not loaded';
  }
  return new Date(value).toLocaleString();
}

function actionClassName(state: ActionState, armed: boolean) {
  if (armed) {
    return 'border-rose-300/30 bg-rose-400/10 text-rose-100';
  }

  switch (state) {
    case 'success':
      return 'border-emerald-300/30 bg-emerald-400/10 text-emerald-100';
    case 'error':
      return 'border-red-300/30 bg-red-400/10 text-red-100';
    case 'running':
      return 'border-cyan-300/30 bg-cyan-400/10 text-cyan-100';
    default:
      return 'border-white/10 bg-white/5 text-zinc-200';
  }
}

function splitLines(value: string) {
  return value
    .split('\n')
    .map((item) => item.trim())
    .filter(Boolean)
    .slice(0, 5);
}
