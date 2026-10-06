import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import {
  fetchOBSStatus,
  fetchReadiness,
  fetchStreamSession,
  fetchTwitchStatus,
  type OBSStatus,
  type ReadinessReport,
  type StreamSessionResponse,
  type TwitchStatus,
} from '../../lib/api';
import { loadOperatorSecret } from '../../lib/dashboardConfig';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import type { Tone } from './ui';

// One status fetch per dashboard page load, shared by the top bar and pages.
// OBS and readiness checks open a WebSocket on the API side, so this refreshes
// on demand rather than on a timer.

export type DeskStatus = {
  apiBaseUrl: string;
  operatorSecret: string;
  twitch: TwitchStatus | null;
  obs: OBSStatus | null;
  readiness: ReadinessReport | null;
  session: StreamSessionResponse | null;
  loading: boolean;
  checkedAt: string;
  errors: { twitch: string; obs: string; readiness: string; session: string };
  refresh: () => void;
  setSession: (session: StreamSessionResponse) => void;
};

const emptyErrors = { twitch: '', obs: '', readiness: '', session: '' };

const DeskStatusContext = createContext<DeskStatus | null>(null);

export function DeskStatusProvider({ children }: { children: ReactNode }) {
  const [apiBaseUrl] = useState(() => getOverlayApiBaseUrl());
  const [operatorSecret] = useState(() => loadOperatorSecret());
  const [twitch, setTwitch] = useState<TwitchStatus | null>(null);
  const [obs, setOBS] = useState<OBSStatus | null>(null);
  const [readiness, setReadiness] = useState<ReadinessReport | null>(null);
  const [session, setSession] = useState<StreamSessionResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [checkedAt, setCheckedAt] = useState('');
  const [errors, setErrors] = useState(emptyErrors);
  const [generation, setGeneration] = useState(0);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      setLoading(true);
      const [twitchResult, obsResult, readinessResult, sessionResult] = await Promise.allSettled([
        fetchTwitchStatus(apiBaseUrl),
        fetchOBSStatus(apiBaseUrl),
        fetchReadiness(apiBaseUrl),
        fetchStreamSession(apiBaseUrl, operatorSecret),
      ]);
      if (cancelled) return;

      setTwitch(twitchResult.status === 'fulfilled' ? twitchResult.value : null);
      setOBS(obsResult.status === 'fulfilled' ? obsResult.value : null);
      setReadiness(readinessResult.status === 'fulfilled' ? readinessResult.value : null);
      setSession(sessionResult.status === 'fulfilled' ? sessionResult.value : null);
      setErrors({
        twitch: twitchResult.status === 'rejected' ? 'Twitch status unavailable.' : '',
        obs: obsResult.status === 'rejected' ? 'OBS status unavailable.' : '',
        readiness: readinessResult.status === 'rejected' ? 'Readiness check unavailable.' : '',
        session: sessionResult.status === 'rejected' ? 'Stream session status unavailable.' : '',
      });
      setCheckedAt(new Date().toISOString());
      setLoading(false);
    }

    void load();
    return () => {
      cancelled = true;
    };
  }, [apiBaseUrl, operatorSecret, generation]);

  const refresh = useCallback(() => setGeneration((current) => current + 1), []);

  const value = useMemo<DeskStatus>(
    () => ({ apiBaseUrl, operatorSecret, twitch, obs, readiness, session, loading, checkedAt, errors, refresh, setSession }),
    [apiBaseUrl, operatorSecret, twitch, obs, readiness, session, loading, checkedAt, errors, refresh],
  );

  return <DeskStatusContext.Provider value={value}>{children}</DeskStatusContext.Provider>;
}

export function useDeskStatus() {
  const value = useContext(DeskStatusContext);
  if (!value) {
    throw new Error('useDeskStatus must be used inside DashboardShell');
  }
  return value;
}

export type OnAirState = 'live' | 'rehearsal' | 'off';

export function onAirState(session: StreamSessionResponse | null): OnAirState {
  const current = session?.current;
  if (!current || current.endedAt) return 'off';
  return current.kind === 'live' ? 'live' : 'rehearsal';
}

export function twitchLamp(status: TwitchStatus | null): { tone: Tone; value: string } {
  if (!status) return { tone: 'neutral', value: 'unknown' };
  if (status.authenticated) return { tone: 'ok', value: status.streamerLogin || 'signed in' };
  if (status.configured) return { tone: 'warn', value: 'needs login' };
  return { tone: 'bad', value: 'not configured' };
}

export function eventSubLamp(status: TwitchStatus | null): { tone: Tone; value: string } {
  const eventSub = status?.eventSub;
  if (!eventSub) return { tone: 'neutral', value: 'unknown' };
  if (eventSub.connected) return { tone: 'ok', value: `${eventSub.subscriptionsMade}/${eventSub.subscriptionsPlan} subs` };
  if (!eventSub.enabled) return { tone: 'neutral', value: 'disabled' };
  return { tone: 'bad', value: 'disconnected' };
}

export function obsLamp(status: OBSStatus | null): { tone: Tone; value: string } {
  if (!status) return { tone: 'neutral', value: 'unknown' };
  if (!status.configured) return { tone: 'bad', value: 'not configured' };
  if (status.reachable && status.authenticated) return { tone: 'ok', value: 'connected' };
  if (status.reachable) return { tone: 'warn', value: 'auth failed' };
  return { tone: 'bad', value: 'offline' };
}

export function readinessLamp(report: ReadinessReport | null): { tone: Tone; value: string } {
  if (!report) return { tone: 'neutral', value: 'unknown' };
  if (report.status === 'ok') return { tone: 'ok', value: 'ready' };
  const degraded = report.checks.filter((check) => check.status !== 'ok').length;
  return { tone: 'warn', value: `${degraded} degraded` };
}
