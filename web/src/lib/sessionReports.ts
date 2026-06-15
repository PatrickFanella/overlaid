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

export function formatSessionReportMarkdown(report: StreamSessionReport) {
  const degradedChecks = report.snapshots.flatMap((snapshot) => snapshot.checks.filter((check) => check.status !== 'ok').map((check) => `- ${snapshot.phase}: ${check.name} — ${check.hint || check.status}`));
  const events = report.events.map((event) => `- ${new Date(event.createdAt).toLocaleString()} · ${event.type} · ${event.source}: ${event.title || event.message || event.id}`);

  return [
    `# ${report.session.title}`,
    '',
    `- ID: ${report.session.id}`,
    `- Kind: ${report.session.kind}`,
    `- Started: ${new Date(report.session.startedAt).toLocaleString()}`,
    `- Ended: ${report.session.endedAt ? new Date(report.session.endedAt).toLocaleString() : 'still open'}`,
    `- Events: ${report.session.eventCount} total, ${report.events.length} exported`,
    `- Readiness snapshots: ${report.snapshots.length}`,
    '',
    '## Degraded readiness checks',
    '',
    degradedChecks.length > 0 ? degradedChecks.join('\n') : 'None.',
    '',
    '## Events',
    '',
    events.length > 0 ? events.join('\n') : 'No events exported.',
    '',
  ].join('\n');
}

export function sessionReportFilename(report: StreamSessionReport, extension: 'json' | 'md') {
  const safeTitle = report.session.title.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'session-report';
  return `${safeTitle}-${report.session.id.slice(0, 8)}.${extension}`;
}
