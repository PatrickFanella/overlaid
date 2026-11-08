import { afterEach, describe, expect, it, vi } from 'vitest';
import { decodeCommandMigrationCatalogue, decodeCommandMigrationItem, listCommandMigration, previewCommandMigration, type CommandMigrationItem } from './commandMigration';
import { emptyTemplateValues } from './templateConversion';

const scope = { accountId: '11111111-1111-4111-8111-111111111111', tenantId: '22222222-2222-4222-8222-222222222222', csrf: 'fixture-csrf', baseURL: 'http://fixture.test' };
const command: CommandMigrationItem = { id: 'legacy-command', name: 'hello', enabled: true, action: 'ticker', responseMode: 'both', roleRequired: 'supporter', cooldownSeconds: 37, snapshot: 'a'.repeat(64), kind: 'template' };
const source = 'Hi {user}';
const preview = () => ({ state: 'command_preview_only', status: 'template_review', command, template: { state: 'preview_only', originalSyntax: 1, proposedSyntax: 2, originalSource: source, proposedSource: 'Hi $(user)', legacyOutput: 'Hi Viewer', proposedOutput: 'Hi Viewer', different: false, warnings: [{ code: 'output_policy', start: 0, end: source.length }], diagnostics: [] } });
afterEach(() => vi.unstubAllGlobals());

describe('saved command migration reads', () => {
  it('retains separate saved aliases and strips extra private fields', () => {
    const alias = { ...command, id: 'alias-command', name: 'alias', snapshot: 'b'.repeat(64), response: 'PRIVATE' };
    const items = decodeCommandMigrationCatalogue({ state: 'command_catalogue_only', commands: [alias, command] });
    expect(items.map(x => x.id)).toEqual(['alias-command', 'legacy-command']);
    expect(JSON.stringify(items)).not.toContain('PRIVATE');
    expect(decodeCommandMigrationCatalogue({ state: 'command_catalogue_only', commands: [] })).toEqual([]);
  });

  it('refuses partial, unordered, duplicate, malformed or contradictory catalogues', () => {
    const other = { ...command, id: 'other-command', name: 'another' };
    for (const commands of [[command, command], [command, other], [{ ...command, name: '<img>' }], [{ ...command, id: 'bad\nidentity' }], [{ ...command, id: 'é'.repeat(65) }], [{ ...command, enabled: 1 }], [{ ...command, cooldownSeconds: 3601 }], [{ ...command, snapshot: 'A'.repeat(64) }], [{ ...command, action: 'unknown' }], [{ ...command, kind: 'editorial' }], [{ ...command, action: 'newsroom_current', kind: 'editorial', responseMode: 'both' }], Array.from({ length: 1001 }, () => command)]) {
      expect(() => decodeCommandMigrationCatalogue({ state: 'command_catalogue_only', commands })).toThrow('Invalid saved-command');
    }
    expect(() => decodeCommandMigrationCatalogue({ state: 'partial', commands: [command] })).toThrow();
  });

  it('recognizes all five Newsroom actions as editorial work', () => {
    for (const action of ['newsroom_submit', 'newsroom_capture', 'newsroom_current', 'newsroom_question', 'newsroom_raffle']) {
      expect(decodeCommandMigrationItem({ ...command, action, responseMode: 'chat', kind: 'editorial' }).kind).toBe('editorial');
    }
  });

  it('sends only the saved identity and supplied sample values, never a browser template', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => preview() });
    vi.stubGlobal('fetch', fetchMock);
    const signal = new AbortController().signal;
    const values = { ...emptyTemplateValues(), user: 'Viewer' };
    const result = await previewCommandMigration(scope, command, values, signal);
    expect(result.originalSource).toBe(source);
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ commandId: command.id, snapshot: command.snapshot, values });
    expect(fetchMock.mock.calls[0][0]).toBe(`http://fixture.test/api/tenants/${scope.tenantId}/automations/preview-command-migration`);
    expect(fetchMock.mock.calls[0][1].headers).not.toHaveProperty('X-Overlay-Operator-Secret');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('rejects crossed metadata and oversized or malformed comparisons', async () => {
    const original = preview();
    for (const patch of [
      { state: 'selected' }, { status: 'editorial_contract_required' }, { command: { ...command, snapshot: 'b'.repeat(64) } }, { command: { ...command, roleRequired: 'viewer' } },
      { template: { ...original.template, originalSource: 'x'.repeat(501) } }, { template: { ...original.template, different: true } },
      { template: { ...original.template, legacyOutput: 'x'.repeat(1601), different: true } },
    ]) {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ...original, ...patch }) }));
      await expect(previewCommandMigration(scope, command, emptyTemplateValues(), new AbortController().signal)).rejects.toThrow();
    }
  });

  it('never retries a stale source or previews editorial responses', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 409 });
    vi.stubGlobal('fetch', fetchMock);
    await expect(previewCommandMigration(scope, command, emptyTemplateValues(), new AbortController().signal)).rejects.toThrow('(409)');
    await expect(previewCommandMigration(scope, { ...command, action: 'newsroom_current', kind: 'editorial', responseMode: 'chat' }, emptyTemplateValues(), new AbortController().signal)).rejects.toThrow();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('fences catalogues and comparisons when their scope request is canceled', async () => {
    for (const read of [listCommandMigration, (s: typeof scope, signal: AbortSignal) => previewCommandMigration(s, command, emptyTemplateValues(), signal)]) {
      const controller = new AbortController();
      vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => { controller.abort(); return { ok: true, json: async () => preview() }; }));
      await expect(read(scope, controller.signal)).rejects.toThrow('canceled');
    }
  });
});
