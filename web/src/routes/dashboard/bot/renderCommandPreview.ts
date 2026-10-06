import type { CommandDraft } from './types';

export function renderCommandPreview(draft: CommandDraft) {
  const values: Record<string, string> = {
    user: 'TestViewer',
    displayname: 'TestViewer',
    channel: 'patrick__eff',
    args: 'sample args',
    command: draft.name.trim().replace(/^!/, '') || 'command',
    'nowplaying.title': 'Example song',
    'nowplaying.artist': 'Example artist',
    'quote:random': 'Example quote — ExampleAuthor',
  };
  const rendered = draft.response.replace(/\{([a-zA-Z0-9_.:-]+)\}/g, (_match, variable: string) => {
    const key = variable.toLowerCase();
    if (key.startsWith('count:')) return '0';
    if (key.startsWith('api:')) return 'API preview';
    return values[key] ?? '';
  }).trim();
  return Array.from(rendered).slice(0, 400).join('').trim();
}
