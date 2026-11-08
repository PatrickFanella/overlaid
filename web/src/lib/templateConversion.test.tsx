import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { decodeTemplatePreview, excerpt, parsePairs } from './templateConversion';
import { TemplatePreviewSummary } from '../routes/dashboard/TemplateConversionDesk';

const source = 'Hi {user} {login} é';
const ok = () => ({ state: 'preview_only', originalSyntax: 1, proposedSyntax: 2, originalSource: source, proposedSource: 'Hi $(user) {login} é', legacyOutput: 'Hi V  é', proposedOutput: 'Hi V {login} é', different: true, warnings: [{ code: 'output_policy', start: 0, end: 20 }, { code: 'unknown_variable', start: 10, end: 17 }], diagnostics: [], extra: 'PRIVATE' });

describe('template conversion preview codec', () => {
  it('accepts a bound response and drops extra fields', () => {
    const r = decodeTemplatePreview(ok(), source);
    expect(r.different).toBe(true);
    expect(JSON.stringify(r)).not.toContain('PRIVATE');
    expect(excerpt(source, r.warnings[1])).toBe('{login}');
  });

  it('refuses mismatched source, syntax, spans, codes or difference flags', () => {
    const cases: Record<string, unknown>[] = [
      { originalSource: 'other' }, { state: 'saved' }, { proposedSyntax: 1 }, { different: false },
      { warnings: [{ code: 'output_policy', start: 0, end: 99 }] }, { warnings: [{ code: 'unknown_variable', start: 0, end: 1 }] },
      { warnings: [{ code: 'output_policy', start: 0, end: 20 }, { code: 'eval', start: 0, end: 1 }] },
      { diagnostics: [{ code: 'unknown_variable', start: 5, end: 2 }] }, { legacyOutput: 1 },
    ];
    for (const patch of cases) expect(() => decodeTemplatePreview({ ...ok(), ...patch }, source)).toThrow('Invalid conversion preview');
    expect(() => decodeTemplatePreview(null, source)).toThrow();
  });

  it('parses supplied pairs strictly', () => {
    expect(parsePairs('deaths=3\n\n wins = -2 ', true)).toEqual({ deaths: 3, wins: -2 });
    expect(parsePairs('song=a=b', false)).toEqual({ song: 'a=b' });
    expect(() => parsePairs('deaths=x', true)).toThrow('whole number');
    expect(() => parsePairs('=3', true)).toThrow();
    expect(() => parsePairs('a=1\na=2', true)).toThrow('unique');
    expect(() => parsePairs(Array.from({ length: 65 }, (_, i) => `k${i}=1`).join('\n'), true)).toThrow('64');
  });

  it('retains prototype-named sample keys through JSON and counts them toward limits', () => {
    for (const numeric of [false, true]) {
      const pairs = parsePairs('__proto__=3\nconstructor=4\ntoString=5', numeric);
      expect(Object.hasOwn(pairs, '__proto__')).toBe(true);
      expect(JSON.parse(JSON.stringify(pairs))).toEqual(JSON.parse(numeric ? '{"__proto__":3,"constructor":4,"toString":5}' : '{"__proto__":"3","constructor":"4","toString":"5"}'));
      expect(() => parsePairs('__proto__=1\n__proto__=2', numeric)).toThrow('unique');
      expect(() => parsePairs(['__proto__=1', ...Array.from({ length: 64 }, (_, i) => `k${i}=1`)].join('\n'), numeric)).toThrow('64');
    }
  });

  it('renders outputs as escaped text without save controls', () => {
    const r = decodeTemplatePreview({ ...ok(), proposedOutput: '<b>x</b>', different: true }, source);
    const html = renderToStaticMarkup(<TemplatePreviewSummary result={r} />);
    expect(html).not.toContain('<b>x</b>'); expect(html).not.toContain('<button'); expect(html).toContain('preview only');
  });
});
