import { versionRequest, type VersionScope } from './automationVersions';
import { decodeTemplatePreview, type TemplatePreview, type TemplateValues } from './templateConversion';

export type CommandMigrationItem = { id: string; name: string; enabled: boolean; action: string; responseMode: string; roleRequired: string; cooldownSeconds: number; snapshot: string; kind: 'template' | 'editorial' };
const actions = new Set(['ticker', 'alert', 'newsroom_submit', 'newsroom_capture', 'newsroom_current', 'newsroom_question', 'newsroom_raffle']);
const roles = new Set(['viewer', 'supporter', 'moderator', 'broadcaster', 'operator', 'system']);
const encoder = new TextEncoder();
const bytes = (value: string) => encoder.encode(value).length;
const fail = (): never => { throw new Error('Invalid saved-command preview response. Refresh the catalogue and select the saved command again.'); };
function record(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) return fail(); return value as Record<string, unknown>; }

export function decodeCommandMigrationItem(value: unknown): CommandMigrationItem {
  const r = record(value);
  if (typeof r.id !== 'string' || !r.id || bytes(r.id) > 128 || r.id.trim() !== r.id || [...r.id].some(c => { const code = c.charCodeAt(0); return code < 32 || code >= 127 && code <= 159; }) || typeof r.name !== 'string' || !/^[a-z0-9][a-z0-9_-]{0,31}$/.test(r.name)) return fail();
  if (typeof r.enabled !== 'boolean' || typeof r.action !== 'string' || !actions.has(r.action) || typeof r.responseMode !== 'string' || !['overlay', 'chat', 'both'].includes(r.responseMode) || typeof r.roleRequired !== 'string' || !roles.has(r.roleRequired) || !Number.isSafeInteger(r.cooldownSeconds) || Number(r.cooldownSeconds) < 0 || Number(r.cooldownSeconds) > 3600 || typeof r.snapshot !== 'string' || !/^[a-f0-9]{64}$/.test(r.snapshot)) return fail();
  const kind = r.action.startsWith('newsroom_') ? 'editorial' : 'template';
  if (r.kind !== kind || kind === 'editorial' && r.responseMode !== 'chat') return fail();
  return { id: r.id, name: r.name, enabled: r.enabled, action: r.action, responseMode: r.responseMode, roleRequired: r.roleRequired, cooldownSeconds: Number(r.cooldownSeconds), snapshot: r.snapshot, kind };
}

export function decodeCommandMigrationCatalogue(value: unknown): CommandMigrationItem[] {
  const r = record(value);
  if (r.state !== 'command_catalogue_only' || !Array.isArray(r.commands) || r.commands.length > 1000) return fail();
  const items = r.commands.map(decodeCommandMigrationItem);
  const ids = new Set<string>(); const names = new Set<string>(); let previous = '';
  for (const item of items) {
    if (ids.has(item.id) || names.has(item.name) || previous && item.name < previous) return fail();
    ids.add(item.id); names.add(item.name); previous = item.name;
  }
  return items;
}

export async function listCommandMigration(scope: VersionScope, signal: AbortSignal): Promise<CommandMigrationItem[]> {
  return decodeCommandMigrationCatalogue(await versionRequest(scope, 'list-command-migration', {}, signal));
}

// The browser submits an identity and sample values; only the server supplies
// the saved template. Reports stay in memory and grant no migration permission.
export async function previewCommandMigration(scope: VersionScope, item: CommandMigrationItem, values: TemplateValues, signal: AbortSignal): Promise<TemplatePreview> {
  const selected = decodeCommandMigrationItem(item);
  if (selected.kind !== 'template') return fail();
  const r = record(await versionRequest(scope, 'preview-command-migration', { commandId: selected.id, snapshot: selected.snapshot, values }, signal));
  if (r.state !== 'command_preview_only' || r.status !== 'template_review' || JSON.stringify(decodeCommandMigrationItem(r.command)) !== JSON.stringify(selected)) return fail();
  const template = record(r.template);
  if (typeof template.originalSource !== 'string' || !template.originalSource || bytes(template.originalSource) > 500) return fail();
  const result = decodeTemplatePreview(template, template.originalSource);
  if (bytes(result.legacyOutput) > 1600) return fail();
  return result;
}
