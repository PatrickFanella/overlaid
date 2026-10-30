import { useEffect, useRef, useState } from 'react';
import type { VersionScope } from '../../lib/automationVersions';
import { emptyTemplateValues, excerpt, parsePairs, previewTemplate, type TemplatePreview, type TemplateValues } from '../../lib/templateConversion';
import { Empty, Field, KeyValues, Notice, Panel, Tag } from './ui';

const warningLabels: Record<string, string> = {
  output_policy: 'Output policy differs: legacy trims and caps at 400 characters; syntax 2 keeps whitespace up to 8 KiB.',
  unknown_variable: 'Unknown token: legacy removes it; syntax 2 keeps it as literal text.',
  escaped_brace: 'Escaped token: legacy still substitutes it; syntax 2 keeps it literally.',
  literal_call: 'Existing $( text is escaped so syntax 2 shows it literally.',
};
const textFields: [keyof TemplateValues, string][] = [['user', 'Login'], ['displayName', 'Display name'], ['channel', 'Channel'], ['command', 'Command'], ['args', 'Arguments'], ['nowPlayingTitle', 'Now playing title'], ['nowPlayingArtist', 'Now playing artist'], ['quoteRandom', 'Quote text']];

// Remounted with its parent on account, workspace or CSRF changes. Source,
// values and outputs stay in memory and are never saved.
export function TemplateConversionDesk({ scope }: { scope: VersionScope }) {
  const [source, setSource] = useState('');
  const [values, setValues] = useState<TemplateValues>(emptyTemplateValues);
  const [counts, setCounts] = useState('');
  const [apis, setApis] = useState('');
  const [result, setResult] = useState<TemplatePreview | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const controller = useRef<AbortController | null>(null);
  const mounted = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; controller.current?.abort(); }; }, []);
  // Edits discard any in-flight or shown result: it no longer matches the inputs.
  function edit(apply: () => void) {
    const current = controller.current;
    if (current) { controller.current = null; current.abort(); setBusy(false); }
    apply(); setResult(null); setError('');
  }
  async function preview() {
    if (controller.current) return;
    let supplied: TemplateValues;
    try { supplied = { ...values, counts: parsePairs(counts, true) as Record<string, number>, apiVariables: parsePairs(apis, false) as Record<string, string> }; }
    catch (e) { setError(e instanceof Error ? e.message : 'Invalid supplied values.'); return; }
    const current = new AbortController(); controller.current = current; setBusy(true); setError(''); setResult(null);
    const timeout = window.setTimeout(() => current.abort(), 15000);
    try {
      const next = await previewTemplate(scope, source, supplied, current.signal);
      if (mounted.current && controller.current === current && !current.signal.aborted) setResult(next);
    } catch (e) {
      if (mounted.current && controller.current === current) setError(current.signal.aborted ? 'Preview canceled or timed out. Nothing was saved.' : (e instanceof Error && /\((400|422)\)/.test(e.message) ? 'The template or supplied values exceed preview limits (4 KiB source, 100 tokens, 64 entries). Nothing was saved.' : 'Conversion preview failed. Nothing was saved.'));
    } finally {
      window.clearTimeout(timeout);
      if (controller.current === current) { controller.current = null; if (mounted.current) setBusy(false); }
    }
  }
  return <Panel title="Template conversion preview" description="Compare a legacy brace template with its proposed syntax-2 text using values you type here. Nothing is saved, selected or sent, and no counter, API or now-playing lookup runs.">
    <Field label="Legacy template" hint="Up to 4 KiB, for example: Welcome {user}! Deaths: {count:deaths}"><textarea className="desk-input min-h-24 w-full font-mono text-sm" rows={3} maxLength={4096} spellCheck={false} value={source} onChange={e => edit(() => setSource(e.target.value))} /></Field>
    <div className="mt-3 grid gap-3 sm:grid-cols-2">{textFields.map(([key, label]) => <Field key={key} label={label}><input className="desk-input w-full" value={values[key] as string} onChange={e => edit(() => setValues({ ...values, [key]: e.target.value }))} /></Field>)}</div>
    <div className="mt-3 grid gap-3 sm:grid-cols-2">
      <Field label="Counters" hint="One name=number per line. Missing counters render as 0."><textarea className="desk-input w-full font-mono text-sm" rows={3} spellCheck={false} value={counts} onChange={e => edit(() => setCounts(e.target.value))} /></Field>
      <Field label="API values" hint="One name=text per line. Missing values render empty."><textarea className="desk-input w-full font-mono text-sm" rows={3} spellCheck={false} value={apis} onChange={e => edit(() => setApis(e.target.value))} /></Field>
    </div>
    <div className="mt-3 flex flex-wrap gap-2"><button className="desk-btn desk-btn--primary" disabled={busy} onClick={() => void preview()}>Preview conversion</button></div>
    {busy ? <p role="status" className="mt-3">Comparing outputs…</p> : null}
    {error ? <div className="mt-3"><Notice tone="bad" role="alert">{error}</Notice></div> : null}
    {result ? <TemplatePreviewSummary result={result} /> : !busy && !error ? <div className="mt-3"><Empty>Enter a template and preview it. Equal outputs here are fixture evidence only, not a migration.</Empty></div> : null}
  </Panel>;
}

export function TemplatePreviewSummary({ result }: { result: TemplatePreview }) {
  return <section aria-label="Conversion preview result" className="mt-4 grid min-w-0 gap-3">
    <Tag>{result.different ? 'Outputs differ' : 'Outputs match for these values'} · preview only</Tag>
    <div className="break-all"><KeyValues items={[['Proposed syntax-2 template', <code key="p" className="whitespace-pre-wrap">{result.proposedSource}</code>], ['Legacy output', <code key="l" className="whitespace-pre-wrap">{result.legacyOutput}</code>], ['Proposed output', <code key="n" className="whitespace-pre-wrap">{result.proposedOutput}</code>]]} /></div>
    <ul className="grid gap-2 text-sm">{result.warnings.map((w, i) => <li key={i} className="desk-sub min-w-0 break-all">{warningLabels[w.code]}{w.code !== 'output_policy' ? <> <code>{excerpt(result.originalSource, w)}</code></> : null}</li>)}</ul>
    {result.diagnostics.length ? <ul className="grid gap-2 text-sm">{result.diagnostics.map((d, i) => <li key={i} className="desk-sub min-w-0 break-all">Syntax 2 · {d.code} · <code>{excerpt(result.proposedSource, d)}</code></li>)}</ul> : null}
    <p className="text-sm">Saving or selecting a converted version is separate work and is not available here.</p>
  </section>;
}
