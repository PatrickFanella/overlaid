import type { ReadinessReport } from '../../../lib/api';
import { Empty, Notice, Panel, Tag } from '../ui';

type ReadinessPanelProps = {
  report: ReadinessReport | null;
  loading: boolean;
  error: string;
};

function formatCheckName(name: string) {
  return name.replaceAll('_', ' ');
}

export function ReadinessPanel({ report, loading, error }: ReadinessPanelProps) {
  const degraded = report?.checks.filter((check) => check.status !== 'ok') ?? [];

  return (
    <Panel
      kicker="Readiness"
      title="Backend readiness"
      description={
        <>
          Mirrors <code>/readyz</code>. Degraded means the operator needs to look, not that the API is down.
        </>
      }
      actions={<Tag tone={loading ? 'neutral' : report?.status === 'ok' ? 'ok' : 'warn'}>{loading ? 'checking' : report?.status ?? 'unknown'}</Tag>}
    >
      {error ? <Notice tone="bad">{error}</Notice> : null}

      {degraded.length > 0 ? (
        <div className="grid gap-2">
          <p className="desk-panel__kicker">Operator incidents</p>
          {degraded.map((check) => (
            <Notice key={check.name} tone="warn">
              <strong>{formatCheckName(check.name)}</strong> · {check.hint || 'Check the production recovery runbook.'}
            </Notice>
          ))}
        </div>
      ) : null}

      {report ? (
        <ul className="grid gap-x-6 gap-y-1 sm:grid-cols-2">
          {report.checks.map((check) => (
            <li key={check.name} className={`desk-lamp desk-lamp--${check.status === 'ok' ? 'ok' : 'warn'}`}>
              {formatCheckName(check.name)} <b>{check.status}</b>
            </li>
          ))}
        </ul>
      ) : !loading ? (
        <Empty>Readiness report unavailable.</Empty>
      ) : null}
    </Panel>
  );
}
