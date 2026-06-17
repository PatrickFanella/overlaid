import type { CommandDraft } from './types';

export function renderCommandPreview(draft: CommandDraft) {
  const values: Record<string, string> = {
    user: 'TestViewer',
    displayName: 'TestViewer',
    channel: 'patrick__eff',
    args: 'sample args',
    command: draft.name.trim().replace(/^!/, '') || 'command',
    'nowPlaying.title': '',
    'nowPlaying.artist': '',
    'quote:random': 'Example quote — ExampleAuthor',
  };
  return draft.response.replace(/\{([^{}]+)\}/g, (_match, key: string) => {
    if (key.startsWith('count:')) return '0';
    if (key.startsWith('api:')) return 'API preview';
    return values[key] ?? '';
  }).trim().slice(0, 400);
}
