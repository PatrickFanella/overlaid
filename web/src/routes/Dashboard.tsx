import { FormEvent, useEffect, useMemo, useState } from 'react';
import { apiUrl, clearOverlays, fetchTwitchStatus, sendTestAlert, sendWebhook, type AlertPayload, type TwitchStatus } from '../lib/api';
import { getOverlayApiBaseUrl } from '../lib/eventStream';

const overlayRoutes = ['/overlay/alerts', '/overlay/chat', '/overlay/goals', '/overlay/now-playing', '/overlay/ticker', '/overlay/brb', '/overlay/debug'] as const;

const defaultAlert: AlertPayload = {
  title: 'Dashboard test',
  message: 'Overlay command center is live',
  severity: 'success',
};

type ActionState = 'idle' | 'running' | 'success' | 'error';

export function DashboardPage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const [status, setStatus] = useState<TwitchStatus | null>(null);
  const [statusError, setStatusError] = useState('');
  const [actionState, setActionState] = useState<ActionState>('idle');
  const [actionMessage, setActionMessage] = useState('');
  const [alert, setAlert] = useState(defaultAlert);
  const [webhookSource, setWebhookSource] = useState('n8n');
  const [webhookSecret, setWebhookSecret] = useState('');

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

  const twitchLoginUrl = useMemo(() => apiUrl(status?.loginUrl || '/api/auth/twitch/login', apiBaseUrl), [apiBaseUrl, status?.loginUrl]);

  async function runAction(label: string, action: () => Promise<void>) {
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
    void runAction('Send test alert', () => sendTestAlert(alert, apiBaseUrl));
  }

  function submitWebhook(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void runAction(`Send ${webhookSource} webhook`, () => sendWebhook(webhookSource, alert, apiBaseUrl, webhookSecret));
  }

  return (
    <main className="min-h-screen bg-[radial-gradient(circle_at_top_left,rgba(217,70,239,0.20),transparent_34%),linear-gradient(135deg,#09090b,#111827_55%,#020617)] px-6 py-8 text-zinc-100">
      <section className="mx-auto max-w-6xl">
        <header className="flex flex-col gap-5 rounded-3xl border border-white/10 bg-white/[0.06] p-6 shadow-2xl shadow-black/30 backdrop-blur md:flex-row md:items-end md:justify-between">
          <div>
            <p className="text-xs uppercase tracking-[0.35em] text-fuchsia-200">over-laid command center</p>
            <h1 className="mt-3 text-4xl font-semibold tracking-tight text-white">Dashboard</h1>
            <p className="mt-2 max-w-2xl text-sm text-zinc-300">Control overlays, check Twitch auth, and fire test events without leaving the browser.</p>
          </div>
          <div className="rounded-2xl border border-white/10 bg-black/30 px-4 py-3 text-sm text-zinc-300">
            <span className="block text-xs uppercase tracking-[0.25em] text-zinc-500">API</span>
            <span className="font-mono text-zinc-100">{apiBaseUrl}</span>
          </div>
        </header>

        <div className="mt-6 grid gap-6 lg:grid-cols-[1.05fr_0.95fr]">
          <section className="rounded-3xl border border-white/10 bg-black/35 p-6 shadow-2xl shadow-black/25 backdrop-blur">
            <div className="flex items-start justify-between gap-4">
              <div>
                <p className="text-xs uppercase tracking-[0.3em] text-cyan-200">Twitch</p>
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
              <a className="rounded-full border border-fuchsia-300/40 bg-fuchsia-300/10 px-4 py-2 text-sm font-medium text-fuchsia-100 transition hover:bg-fuchsia-300/20" href={twitchLoginUrl}>
                Twitch login
              </a>
              <a className="rounded-full border border-white/15 bg-white/5 px-4 py-2 text-sm text-zinc-100 transition hover:bg-white/10" href={apiUrl('/api/auth/twitch/status', apiBaseUrl)}>
                Status JSON
              </a>
            </div>
          </section>

          <section className="rounded-3xl border border-white/10 bg-black/35 p-6 shadow-2xl shadow-black/25 backdrop-blur">
            <p className="text-xs uppercase tracking-[0.3em] text-fuchsia-200">Overlay controls</p>
            <h2 className="mt-2 text-2xl font-semibold text-white">Fire events</h2>
            <form className="mt-5 grid gap-4" onSubmit={submitTestAlert}>
              <TextInput label="Title" value={alert.title} onChange={(title) => setAlert((current) => ({ ...current, title }))} />
              <TextInput label="Message" value={alert.message} onChange={(message) => setAlert((current) => ({ ...current, message }))} />
              <label className="grid gap-2 text-sm text-zinc-300">
                Severity
                <select className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-fuchsia-300/30 focus:ring-4" value={alert.severity} onChange={(event) => setAlert((current) => ({ ...current, severity: event.target.value as AlertPayload['severity'] }))}>
                  <option value="info">info</option>
                  <option value="success">success</option>
                  <option value="warning">warning</option>
                  <option value="error">error</option>
                </select>
              </label>
              <div className="flex flex-wrap gap-3">
                <button className="rounded-full bg-fuchsia-300 px-5 py-2 text-sm font-semibold text-zinc-950 transition hover:bg-fuchsia-200" type="submit">
                  Send test alert
                </button>
                <button className="rounded-full border border-white/15 bg-white/5 px-5 py-2 text-sm text-zinc-100 transition hover:bg-white/10" type="button" onClick={() => void runAction('Clear overlays', () => clearOverlays(apiBaseUrl))}>
                  Clear overlays
                </button>
              </div>
            </form>

            <form className="mt-5 flex flex-col gap-3 border-t border-white/10 pt-5 sm:flex-row" onSubmit={submitWebhook}>
              <label className="grid flex-1 gap-2 text-sm text-zinc-300">
                Webhook source
                <input className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4" value={webhookSource} onChange={(event) => setWebhookSource(event.target.value)} />
              </label>
              <label className="grid flex-1 gap-2 text-sm text-zinc-300">
                Webhook secret
                <input className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4" type="password" value={webhookSecret} onChange={(event) => setWebhookSecret(event.target.value)} placeholder="optional" />
              </label>
              <button className="self-end rounded-full border border-cyan-200/40 bg-cyan-200/10 px-5 py-3 text-sm font-medium text-cyan-100 transition hover:bg-cyan-200/20" type="submit">
                Send webhook
              </button>
            </form>

            {actionMessage ? <p className={`mt-4 rounded-2xl border px-4 py-3 text-sm ${actionClassName(actionState)}`}>{actionMessage}</p> : null}
          </section>
        </div>

        <section className="mt-6 rounded-3xl border border-white/10 bg-black/30 p-6 shadow-2xl shadow-black/25 backdrop-blur">
          <p className="text-xs uppercase tracking-[0.3em] text-zinc-400">OBS routes</p>
          <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {overlayRoutes.map((route) => (
              <a key={route} className="rounded-2xl border border-white/10 bg-white/[0.04] p-4 transition hover:border-fuchsia-300/40 hover:bg-white/[0.08]" href={route}>
                <p className="font-mono text-sm text-fuchsia-100">{route}</p>
                <p className="mt-2 text-xs text-zinc-400">Open browser source</p>
              </a>
            ))}
          </div>
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
      {label}
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

function actionClassName(state: ActionState) {
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
