import { useEffect, useState } from 'react';
import { fetchTwitchInteractions, type TwitchInteractionRules } from '../../../lib/api';
import { createEmptyTwitchInteractionRules } from '../../../lib/twitchInteractionRules';
import { TwitchInteractionRulesEditor } from './twitch-interactions/TwitchInteractionRulesEditor';
import { TwitchRewardQueueManager } from './twitch-interactions/TwitchRewardQueueManager';
import { Panel, Tag } from '../ui';

type TwitchInteractionsManagerProps = {
  apiBaseUrl?: string;
  operatorSecret: string;
};

export function TwitchInteractionsManager({ apiBaseUrl, operatorSecret }: TwitchInteractionsManagerProps) {
  const [rules, setRules] = useState<TwitchInteractionRules>(() => createEmptyTwitchInteractionRules());
  const [loadError, setLoadError] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;

    setLoading(true);
    fetchTwitchInteractions({ baseUrl: apiBaseUrl, operatorSecret })
      .then((response) => {
        if (cancelled) return;
        setRules(response.rules ?? createEmptyTwitchInteractionRules());
        setLoadError(response.loadError ?? '');
      })
      .catch((error: unknown) => {
        if (cancelled) return;
        setLoadError(error instanceof Error ? error.message : 'Failed to load Twitch interaction rules.');
      })
      .finally(() => {
        if (cancelled) return;
        setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [apiBaseUrl, operatorSecret]);

  return (
    <div className="grid gap-4">
      <Panel
        id="twitch-rules"
        kicker="Twitch"
        title="Twitch interaction rules"
        description="Chat aliases and channel point routing are stored on the backend. Operator secret required to save."
        actions={<Tag tone={loading ? 'neutral' : 'ok'}>{loading ? 'Loading rules…' : 'Rules loaded'}</Tag>}
      >
        <TwitchInteractionRulesEditor
          apiBaseUrl={apiBaseUrl}
          operatorSecret={operatorSecret}
          rules={rules}
          onRulesChange={setRules}
          loadError={loadError}
          onLoadErrorChange={setLoadError}
        />
      </Panel>

      <TwitchRewardQueueManager apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} channelPointRules={rules.channelPointRules} />
    </div>
  );
}
