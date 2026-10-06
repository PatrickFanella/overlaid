import { BotWorkspace } from './Bot';
import { TwitchInteractionsManager } from './components/TwitchInteractionsManager';
import { DashboardShell } from './DashboardShell';
import { useDeskStatus } from './deskStatus';
import { MissingSecretNotice } from './ActiveProfile';
import { PageHead } from './ui';

const sections = [
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

  return (
    <>
      <PageHead
        title="Chat and bot"
        lede="Bot commands, timers, and community data, plus moderation with guarded destructive actions. Everything here writes through the operator-protected API."
      />
      <nav className="flex flex-wrap gap-1" aria-label="Chat sections">
        {sections.map(([href, label]) => (
          <a key={href} className="desk-btn desk-btn--small desk-btn--ghost" href={href}>
            {label}
          </a>
        ))}
      </nav>
      <MissingSecretNotice operatorSecret={operatorSecret} />
      <BotWorkspace />
      <TwitchInteractionsManager apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} />
    </>
  );
}
