import { versionRequest, type VersionScope } from './automationVersions';

// Supplied fixture values for an effect-free legacy → syntax-2 comparison.
// Nothing here is saved; the server reads no tenant state and selects nothing.
export type TemplateValues = { user: string; displayName: string; channel: string; args: string; command: string; nowPlayingTitle: string; nowPlayingArtist: string; quoteRandom: string; counts: Record<string, number>; apiVariables: Record<string, string> };
export type TemplateSpan = { code: string; start: number; end: number };
export type TemplatePreview = { originalSource: string; proposedSource: string; warnings: TemplateSpan[]; diagnostics: TemplateSpan[]; legacyOutput: string; proposedOutput: string; different: boolean };

export const emptyTemplateValues = (): TemplateValues => ({ user: '', displayName: '', channel: '', args: '', command: '', nowPlayingTitle: '', nowPlayingArtist: '', quoteRandom: '', counts: {}, apiVariables: {} });

const maxEntries = 64;
const maxName = 128;
const warningCodes = new Set(['output_policy', 'unknown_variable', 'escaped_brace', 'literal_call']);
const diagnosticCodes = new Set(['cancelled', 'deadline', 'output_limit', 'resolution_limit', 'unknown_variable', 'argument_count', 'unavailable_variable']);
const encoder = new TextEncoder();
const bytes = (s: string) => encoder.encode(s).length;

const fail = (): never => { throw new Error('Invalid conversion preview response. Nothing was saved; run the preview again.'); };

// Parses "name=value" lines. Blank lines are ignored; malformed lines refuse.
export function parsePairs(text: string, numeric: boolean): Record<string, number> | Record<string, string> {
  const out: Record<string, number | string> = {};
  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (!line) continue;
    const at = line.indexOf('=');
    const name = at > 0 ? line.slice(0, at).trim() : '';
    const value = at > 0 ? line.slice(at + 1) : '';
    if (!name || name.length > maxName || Object.prototype.hasOwnProperty.call(out, name)) throw new Error(`Each line needs a unique name=value pair (names up to ${maxName} characters).`);
    if (numeric) {
      if (!/^-?\d+$/.test(value.trim()) || !Number.isSafeInteger(Number(value.trim()))) throw new Error(`Counter ${name} needs a whole number.`);
      out[name] = Number(value.trim());
    } else out[name] = value;
    if (Object.keys(out).length > maxEntries) throw new Error(`At most ${maxEntries} entries are allowed.`);
  }
  return out as Record<string, number> | Record<string, string>;
}

function spans(value: unknown, codes: Set<string>, limit: number, sourceBytes: number): TemplateSpan[] {
  if (!Array.isArray(value) || value.length > limit) return fail();
  return value.map(raw => {
    if (!raw || typeof raw !== 'object') return fail();
    const r = raw as Record<string, unknown>;
    if (typeof r.code !== 'string' || !codes.has(r.code) || !Number.isSafeInteger(r.start) || !Number.isSafeInteger(r.end)) return fail();
    const start = Number(r.start); const end = Number(r.end);
    if (start < 0 || end < start || end > sourceBytes) return fail();
    return { code: r.code, start, end };
  });
}

// The response must echo the exact submitted source; extra fields are dropped.
export function decodeTemplatePreview(value: unknown, source: string): TemplatePreview {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return fail();
  const r = value as Record<string, unknown>;
  if (r.state !== 'preview_only' || r.originalSyntax !== 1 || r.proposedSyntax !== 2 || r.originalSource !== source) return fail();
  for (const k of ['proposedSource', 'legacyOutput', 'proposedOutput']) if (typeof r[k] !== 'string') return fail();
  const proposedSource = r.proposedSource as string; const legacyOutput = r.legacyOutput as string; const proposedOutput = r.proposedOutput as string;
  if (typeof r.different !== 'boolean' || r.different !== (legacyOutput !== proposedOutput) || bytes(proposedSource) > 4096 || bytes(proposedOutput) > 8192) return fail();
  const warnings = spans(r.warnings, warningCodes, 100, bytes(source));
  if (!warnings.some(w => w.code === 'output_policy')) return fail();
  return { originalSource: source, proposedSource, warnings, diagnostics: spans(r.diagnostics, diagnosticCodes, 256, bytes(proposedSource)), legacyOutput, proposedOutput, different: r.different };
}

// Byte-span excerpt for display only.
export function excerpt(source: string, span: TemplateSpan): string {
  const b = encoder.encode(source);
  return new TextDecoder().decode(b.slice(span.start, span.end));
}

export async function previewTemplate(scope: VersionScope, source: string, values: TemplateValues, signal: AbortSignal): Promise<TemplatePreview> {
  return decodeTemplatePreview(await versionRequest(scope, 'preview-template-conversion', { source, values }, signal), source);
}
