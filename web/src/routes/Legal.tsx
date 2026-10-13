import { useEffect, useState } from 'react';
import { requestJSON } from '../lib/apiRequest';

type OperatorInfo = { productName?: string; supportContact?: string };
function useOperatorInfo() {
  const [info, setInfo] = useState<OperatorInfo>({ productName: 'overlaid' });
  useEffect(() => { void requestJSON<OperatorInfo>('/api/account', { method: 'GET', errorLabel: 'Operator information' }).then(setInfo).catch(() => {}); }, []);
  return info;
}

export function LegalPage({ kind }: { kind: 'privacy' | 'terms' }) {
  const info = useOperatorInfo();
  return <div className="desk"><main className="desk-page max-w-3xl">
    <h1 className="desk-title">{kind === 'privacy' ? 'Privacy policy' : 'Terms of service'}</h1>
    <p>{info.productName || 'overlaid'} · Updated October 13, 2026</p>
    {kind === 'privacy' ? <>
      <h2>Operator and purpose</h2><p>The operator of this installation provides streaming workspaces, bot configuration, overlays and moderation tools. Account and stream data are used to provide those features, enforce permissions, investigate failures and process deletion requests.</p>
      <h2>Data collected</h2><p>Twitch sign-in records your immutable Twitch user ID and display name. Workspaces store their members and roles, saved commands and settings, chat and stream events, sessions and action receipts. Email addresses are not used to match or link accounts. Connecting a channel stores its granted scopes and encrypted access and refresh tokens. Sign-in alone does not connect your channel.</p>
      <h2>Cookies and storage</h2><p>An HTTP-only session cookie lasts at most 24 hours. A CSRF cookie protects browser changes; OAuth browser state lasts ten minutes and is consumed once. Sign-out revokes the session. Workspace settings and operator action logs may be cached in your browser under the selected workspace; browser exports exclude credentials. Overlay URLs are read credentials and should be shared only with trusted source operators.</p>
      <h2>Retention and deletion</h2><p>Workspace owners choose event and audit retention from 1 to 365 days; the default is 30 days. Expiry runs every minute. Owners can export workspace data, request viewer record deletion and delete hosted workspaces. Viewer deletion removes matching event and action records and clears derived retained overlay state. Workspace deletion revokes credentials and its active sessions before deleting content. Security receipts about deletion retain only the operator actor, action and non-secret metadata for up to 90 days. Backups may retain deleted records until the operator's backup retention ends; restored backups must reapply pending deletion requests before serving users.</p>
      <h2>Access and providers</h2><p>Workspace access follows owner, manager, editor and moderator permissions. Platform requests go to Twitch for the features you grant. Tokens, session identifiers and overlay credential hashes are excluded from workspace exports. The service does not sell chat records or use them for advertising. Kick, Discord, YouTube and billing are planned features and are not enabled by this account implementation.</p>
      <h2>Your requests</h2><p>Use Account and workspace to export data, revoke sessions, disconnect Twitch or manage retention. For account-level, backup or viewer requests that you cannot make through an owner, contact the installation operator: {info.supportContact || 'the person or organization that provided this installation'}.</p>
    </> : <>
      <h2>Use of the service</h2><p>Use your own platform account or a workspace to which you have been granted access. Follow platform rules and applicable law. Do not bypass permissions, send abusive requests, expose credentials or use moderation tools against people outside your authorized channel.</p>
      <h2>Workspace responsibility</h2><p>Owners control membership, platform grants, retention and deletion. Managers and editors have narrower rights; moderators can read moderation data and perform permitted moderation. Keep OBS sources and webhook credentials private, and rotate them when access changes.</p>
      <h2>Media and copyright</h2><p>Streamers decide what media plays and are responsible for permission to use it. Stop media promptly if it receives a copyright or DMCA claim. Open media requests and playback queues are planned features; these terms do not enable them or grant rights to third-party content.</p>
      <h2>Plans and availability</h2><p>Free and paid plan limits are stored as configuration. Billing and purchases are not enabled in this implementation. Self-host installations have unlimited commercial limits while retaining safety and provider limits. The service may pause work when permissions, limits or readiness checks fail. The operator can suspend abusive access and must preserve a way to request applicable data export or deletion.</p>
      <h2>Account changes and support</h2><p>You can sign out, revoke sessions and disconnect a channel. A workspace owner can export or delete hosted workspace data. Contact the installation operator for ownership, account or service questions: {info.supportContact || 'the person or organization that provided this installation'}.</p>
    </>}
    <p><a href="/dashboard/account">Account and workspace</a> · <a href={kind === 'privacy' ? '/terms' : '/privacy'}>{kind === 'privacy' ? 'Terms of service' : 'Privacy policy'}</a> · <a href="/status">Service status</a></p>
  </main></div>;
}

export function PublicStatusPage() {
  const [report, setReport] = useState<{ status: string; productName?: string; checks: Array<{ name: string; status: string }> }>();
  const [error, setError] = useState('');
  useEffect(() => {
    let active = true;
    async function refresh() { try { const value = await requestJSON<typeof report>('/api/public/status', { method: 'GET', errorLabel: 'Service status' }); if (active) { setReport(value); setError(''); } } catch { if (active) setError('Service status is unavailable.'); } }
    void refresh(); const timer = window.setInterval(() => void refresh(), 30000);
    return () => { active = false; window.clearInterval(timer); };
  }, []);
  return <div className="desk"><main className="desk-page max-w-3xl"><h1 className="desk-title">Service status</h1><p role="status">{error || (report ? `${report.productName || 'overlaid'}: ${report.status}` : 'Checking service…')}</p>{report ? <ul>{report.checks.map((check) => <li key={check.name}>{check.name}: {check.status}</li>)}</ul> : null}<p><a href="/dashboard/account">Account</a> · <a href="/privacy">Privacy</a> · <a href="/terms">Terms</a></p></main></div>;
}
