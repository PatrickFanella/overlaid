import type { OverlayEvent } from './events';

// Sample events for `?demo=1`. The dashboard previews and visual QA use these
// so every overlay can be judged without a running API, Twitch, or OBS.

type DemoEvent = Omit<OverlayEvent, 'id' | 'createdAt'>;

const chatters = [
  { name: 'nightshift_dev', color: '#00ff88', badges: ['moderator'] },
  { name: 'zinefiend', color: '#f0ece4', badges: ['subscriber'] },
  { name: 'oddjobs', color: '#c4b5fd', badges: [] },
  { name: 'doorperson', color: '#fbbf24', badges: ['vip'] },
];

const chatLines = [
  'the archive tool is actually so good',
  'clip that',
  'what are we building today?',
  'mutual aid board went live last night btw',
  'that query plan is cursed',
  'back from the show, what did I miss',
];

export const demoEvents: DemoEvent[] = [
  { type: 'alert', source: 'twitch', title: 'New follower', message: 'zinefiend is in the building.', severity: 'info', data: { kind: 'follow' } },
  { type: 'alert', source: 'twitch', title: 'Sub × 6 months', message: 'nightshift_dev: still here, still building.', severity: 'success', data: { kind: 'subscription' } },
  { type: 'alert', source: 'twitch', title: '500 bits', message: 'doorperson: for the door fund.', severity: 'warning', data: { kind: 'bits' } },
  { type: 'alert', source: 'homelab', title: 'Almaz disk at 91%', message: 'Media pool needs attention after stream.', severity: 'error' },
  {
    type: 'alert',
    source: 'demo',
    title: 'Goal update',
    message: 'Door fund moved.',
    severity: 'info',
    data: {
      retainedStateKey: 'goals',
      goals: [
        { id: 'door', label: 'Door fund', current: 340, target: 500, unit: 'usd' },
        { id: 'subs', label: 'Sub goal', current: 18, target: 25, unit: 'subs' },
        { id: 'followers', label: 'Followers', current: 1204, target: 1500, unit: 'followers' },
      ],
    },
  },
  {
    type: 'now_playing',
    source: 'mpris',
    title: 'Night Bus Archive',
    message: 'Low Signal Collective',
    severity: 'info',
    data: { player: 'spotify', title: 'Night Bus Archive', artist: 'Low Signal Collective', album: 'Tapes From The Back Room', playbackStatus: 'Playing', positionMs: 74_000, lengthMs: 212_000 },
  },
  {
    type: 'brb',
    source: 'dashboard',
    title: 'Be right back',
    message: 'Swapping tapes. Chat stays open.',
    severity: 'info',
    data: { active: true, eta: '5 MIN', items: ['Chat stays live', 'Music keeps playing', 'Clips going up on the archive'] },
  },
];

export function buildDemoChatEvent(index: number): DemoEvent {
  const chatter = chatters[index % chatters.length];
  return {
    type: 'chat_message',
    source: 'twitch',
    title: chatter.name,
    message: chatLines[index % chatLines.length],
    severity: 'info',
    data: {
      twitchEvent: {
        chatter_user_name: chatter.name,
        chatter_color: chatter.color,
        badges: chatter.badges.map((setId) => ({ set_id: setId })),
      },
    },
  };
}

export function stampDemoEvent(event: DemoEvent, sequence: number): OverlayEvent {
  return {
    ...event,
    id: `demo-${sequence}`,
    createdAt: new Date().toISOString(),
  };
}
