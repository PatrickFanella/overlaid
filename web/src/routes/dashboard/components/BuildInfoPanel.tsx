import { useEffect, useState } from 'react';
import { fetchBuildInfo, type BuildInfo } from '../../../lib/api';
import { Empty, KeyValues, Notice, Panel, Tag } from '../ui';

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
    <Panel kicker="Release" title="Build info" actions={<Tag>/version</Tag>}>
      {error ? <Notice tone="bad">{error}</Notice> : null}

      {info ? (
        <KeyValues
          items={[
            ['version', info.version],
            ['commit', <code key="commit">{info.commit}</code>],
            ['built', info.builtAt],
            ['runtime', `${info.goVersion} · ${info.os}/${info.arch}`],
          ]}
        />
      ) : !error ? (
        <Empty>Build report unavailable.</Empty>
      ) : null}
    </Panel>
  );
}
