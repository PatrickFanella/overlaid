import type { SessionBotAudit, StreamSessionReport } from './api';

export function formatSessionBotAuditStatus(audit: SessionBotAudit | undefined, verb: 'loaded' | 'exported' = 'loaded') {
  if (!audit) return 'Bot audit not included by this server.';
  if (audit.loadError) return `Bot audit unavailable: ${audit.loadError}`;
  return `${audit.events.length} of ${audit.total} bot actions ${verb}.`;
}

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
    formatSessionBotAuditStatus(report.botAudit),
    degradedChecks.length > 0 ? `Degraded checks: ${degradedChecks.join(', ')}` : 'Degraded checks: none',
  ].join('\n');
}

export function formatSessionReportMarkdown(report: StreamSessionReport) {
  const degradedChecks = report.snapshots.flatMap((snapshot) => snapshot.checks.filter((check) => check.status !== 'ok').map((check) => `- ${snapshot.phase}: ${check.name} — ${check.hint || check.status}`));
  const botActions = report.botAudit?.events.map((event) => `- ${new Date(event.createdAt).toLocaleString()} · ${event.action || event.command || event.trigger || 'bot action'} · ${event.source} · ${event.actorLogin || event.actorRole || 'unknown actor'} · ${event.result} · ${event.reason || 'no reason'}`) ?? [];
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
    '## Bot actions',
    '',
    formatSessionBotAuditStatus(report.botAudit, 'exported'),
    ...(botActions.length > 0 ? ['', ...botActions] : []),
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
