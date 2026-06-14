import { useEffect, useState } from 'react';
import { fetchBuildInfo, type BuildInfo } from '../../../lib/api';

type BuildInfoPanelProps = {
  apiBaseUrl: string;
};

export function BuildInfoPanel({ apiBaseUrl }: BuildInfoPanelProps) {
  const [info, setInfo] = useState<BuildInfo | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    let active = true;

    fetchBuildInfo(apiBaseUrl)
      .then((next) => {
        if (active) {
          setInfo(next);
        }
      })
      .catch((err: unknown) => {
        if (active) {
          setError(err instanceof Error ? err.message : 'Build info unavailable');
        }
      });

    return () => {
      active = false;
    };
  }, [apiBaseUrl]);

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="hud-label">release metadata</p>
          <h2 className="hud-section-title">Build info</h2>
          <p className="hud-copy mt-1 text-xs text-zinc-300">
            Snapshot from <code>/version</code>; safe to render before the client hydrates.
          </p>
        </div>
        <span className="hud-chip hud-chip--square">/version</span>
      </div>

      {error ? <p className="mt-3 text-sm text-rose-300">{error}</p> : null}

      {info ? (
        <dl className="mt-3 grid gap-2 text-xs">
          <div className="hud-terminal-line">
            <dt className="hud-terminal-key">version</dt>
            <dd className="hud-terminal-value">{info.version}</dd>
          </div>
          <div className="hud-terminal-line">
            <dt className="hud-terminal-key">commit</dt>
            <dd className="hud-terminal-value">{info.commit}</dd>
          </div>
          <div className="hud-terminal-line">
            <dt className="hud-terminal-key">built</dt>
            <dd className="hud-terminal-value">{info.builtAt}</dd>
          </div>
          <div className="hud-terminal-line">
            <dt className="hud-terminal-key">runtime</dt>
            <dd className="hud-terminal-value">
              {info.goVersion} · {info.os}/{info.arch}
            </dd>
          </div>
        </dl>
      ) : !error ? (
        <p className="mt-3 text-xs text-zinc-400">Build report unavailable.</p>
      ) : null}
    </section>
  );
}
