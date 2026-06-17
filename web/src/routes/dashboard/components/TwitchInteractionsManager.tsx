import { useEffect, useState } from 'react';
import { fetchTwitchInteractions, type TwitchInteractionRules } from '../../../lib/api';
import { createEmptyTwitchInteractionRules } from '../../../lib/twitchInteractionRules';
import { TwitchInteractionRulesEditor } from './twitch-interactions/TwitchInteractionRulesEditor';
import { TwitchRewardQueueManager } from './twitch-interactions/TwitchRewardQueueManager';

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
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <p className="hud-label">Twitch interaction rules</p>
          <h2 className="hud-section-title">Twitch interaction rules</h2>
          <p className="hud-copy mt-1 text-xs text-zinc-300">Chat aliases and channel point routing live on the backend. Operator secret required to save.</p>
        </div>
        <span className="hud-chip">{loading ? 'Loading rules…' : 'Rules loaded'}</span>
      </div>

      <TwitchInteractionRulesEditor
        apiBaseUrl={apiBaseUrl}
        operatorSecret={operatorSecret}
        rules={rules}
        onRulesChange={setRules}
        loadError={loadError}
        onLoadErrorChange={setLoadError}
      />

      <TwitchRewardQueueManager apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} channelPointRules={rules.channelPointRules} />
    </section>
  );
}
