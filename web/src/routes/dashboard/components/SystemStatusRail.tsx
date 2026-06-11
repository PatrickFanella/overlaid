import { useEffect, useState } from 'react';
import { fetchOBSStatus, fetchTwitchStatus, type OBSStatus, type TwitchStatus } from '../../../lib/api';
import type { DashboardConfig } from '../../../lib/dashboardConfig';

type SystemStatusRailProps = {
  apiBaseUrl: string;
  operatorSecret: string;
  config: DashboardConfig;
};

type LoadState = {
  twitch: TwitchStatus | null;
  obs: OBSStatus | null;
  loading: boolean;
  error: string;
};

const defaultState: LoadState = {
  twitch: null,
  obs: null,
  loading: true,
  error: '',
};

export function SystemStatusRail({ apiBaseUrl, operatorSecret, config }: SystemStatusRailProps) {
  const [state, setState] = useState<LoadState>(defaultState);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      setState((current) => ({ ...current, loading: true, error: '' }));

      const [twitchResult, obsResult] = await Promise.allSettled([fetchTwitchStatus(apiBaseUrl), fetchOBSStatus(apiBaseUrl)]);
      if (cancelled) {
        return;
      }

      setState({
        twitch: twitchResult.status === 'fulfilled' ? twitchResult.value : null,
        obs: obsResult.status === 'fulfilled' ? obsResult.value : null,
        loading: false,
        error:
          twitchResult.status === 'rejected' && obsResult.status === 'rejected'
            ? 'Unable to load Twitch and OBS status.'
            : twitchResult.status === 'rejected'
              ? 'Unable to load Twitch status.'
              : obsResult.status === 'rejected'
                ? 'Unable to load OBS status.'
                : '',
      });
    }

    void load();

    return () => {
      cancelled = true;
    };
  }, [apiBaseUrl]);

  return (
    <section className="hud-panel hud-corners hud-sticky-rail rounded-[1.35rem] p-3">
      <div className="flex flex-wrap items-center justify-between gap-3 px-2 pt-1">
        <div>
          <p className="hud-label">System status</p>
          <p className="mt-2 text-sm text-zinc-300">Live dashboard health for this operator workstation.</p>
        </div>
        <span className="hud-chip">{state.loading ? 'refreshing' : 'ready'}</span>
      </div>

      <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        <StatusItem label="API" value={apiBaseUrl} detail={state.error || 'overlay service'} accent="cyan" />
        <StatusItem label="Twitch" value={renderTwitchStatus(state.twitch)} detail={state.twitch?.channelTarget || 'status unavailable'} accent={state.twitch?.authenticated ? 'emerald' : 'amber'} />
        <StatusItem label="OBS" value={renderObsStatus(state.obs)} detail={state.obs?.url || 'status unavailable'} accent={state.obs?.reachable ? 'emerald' : 'amber'} />
        <StatusItem label="Operator" value={operatorSecret ? 'loaded' : 'missing'} detail={operatorSecret ? 'session secret in browser storage' : 'enter secret in Config'} accent={operatorSecret ? 'emerald' : 'rose'} />
        <StatusItem label="Quick actions" value={String(config.quickActions.length)} detail="editable presets" accent="fuchsia" />
        <StatusItem label="Setup" value={config.setupChecklist.every((item) => item.completed) ? 'complete' : 'in progress'} detail="local checklist state" accent="amber" />
      </div>
    </section>
  );
}

type StatusItemProps = {
  label: string;
  value: string;
  detail: string;
  accent: 'cyan' | 'fuchsia' | 'emerald' | 'amber' | 'rose';
};

function StatusItem({ label, value, detail, accent }: StatusItemProps) {
  return (
    <article className={`hud-panel hud-corners rounded-[1.1rem] p-4 hud-accent-${accent}`}>
      <p className="hud-kicker">{label}</p>
      <p className="mt-2 text-lg font-semibold text-white">{value}</p>
      <p className="mt-2 text-sm text-zinc-300">{detail}</p>
    </article>
  );
}

function renderTwitchStatus(status: TwitchStatus | null) {
  if (!status) {
    return 'unavailable';
  }

  if (status.authenticated) {
    return 'authenticated';
  }

  if (status.configured) {
    return 'needs login';
  }

  return 'not configured';
}

function renderObsStatus(status: OBSStatus | null) {
  if (!status) {
    return 'unavailable';
  }

  if (!status.configured) {
    return 'not configured';
  }

  if (status.reachable && status.authenticated) {
    return 'connected';
  }

  if (status.reachable) {
    return 'reachable';
  }

  return 'offline';
}
