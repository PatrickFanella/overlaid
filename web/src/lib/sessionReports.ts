import type { StreamSessionReport } from './api';

export function formatSessionReportSummary(report: StreamSessionReport) {
  const degradedSnapshots = report.snapshots.filter((snapshot) => snapshot.status !== 'ok');
  const degradedChecks = report.snapshots.flatMap((snapshot) => snapshot.checks.filter((check) => check.status !== 'ok').map((check) => `${snapshot.phase}:${check.name}`));

  return [
    `${report.session.title} (${report.session.kind})`,
    `Session: ${report.session.id}`,
    `Started: ${new Date(report.session.startedAt).toLocaleString()}`,
    report.session.endedAt ? `Ended: ${new Date(report.session.endedAt).toLocaleString()}` : 'Ended: still open',
    `Events: ${report.session.eventCount} total, ${report.events.length} loaded`,
    `Readiness snapshots: ${report.snapshots.length} (${degradedSnapshots.length} degraded)`,
    degradedChecks.length > 0 ? `Degraded checks: ${degradedChecks.join(', ')}` : 'Degraded checks: none',
  ].join('\n');
}
