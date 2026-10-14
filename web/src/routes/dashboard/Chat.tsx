import { BotWorkspace } from './Bot';
import { useAccount } from './accountSession';
import { BotIdentityPanel } from './bot/BotIdentityPanel';
import { TwitchInteractionsManager } from './components/TwitchInteractionsManager';
import { DashboardShell } from './DashboardShell';
import { useDeskStatus } from './deskStatus';
import { MissingSecretNotice } from './ActiveProfile';
import { PageHead } from './ui';

const sections = [
  ['#bot-identity', 'Bot identity'],
  ['#bot-commands', 'Commands'],
  ['#bot-timers', 'Timers'],
  ['#bot-community', 'Counters and quotes'],
  ['#bot-api-variables', 'API variables'],
  ['#bot-moderation', 'Moderation'],
  ['#twitch-rules', 'Twitch rules'],
  ['#reward-queue', 'Reward queue'],
] as const;

export function DashboardChatPage() {
  return (
    <DashboardShell activePath="/dashboard/chat">
      <Chat />
    </DashboardShell>
  );
}

function Chat() {
  const { apiBaseUrl, operatorSecret } = useDeskStatus();
  const { account } = useAccount();

  return (
    <>
      <PageHead
        title="Chat and bot"
        lede="Bot commands, timers, and community data, plus moderation with guarded destructive actions. Changes use the authenticated API and workspace permissions."
      />
      <nav className="flex flex-wrap gap-1" aria-label="Chat sections">
        {sections.map(([href, label]) => (
          <a key={href} className="desk-btn desk-btn--small desk-btn--ghost" href={href}>
            {label}
          </a>
        ))}
      </nav>
      {account.deploymentMode !== 'hosted' ? <MissingSecretNotice operatorSecret={operatorSecret} /> : null}
      <BotIdentityPanel apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} />
      <BotWorkspace />
      <TwitchInteractionsManager apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} />
    </>
  );
}
