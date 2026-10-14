import { useEffect, useState } from 'react';
import { requestJSON } from '../../../lib/apiRequest';
import { useAccount } from '../accountSession';
import { Check, Empty, Notice, Panel, SubPanel, Tag } from '../ui';

type Bot = { id: string; login: string; userId: string; scopes: string[]; revoked: boolean; bound: boolean };
type Shared = { id: string; login: string; userId: string; grantRevision: number; revision: number };
type Consent = { botAccountId: string; capabilities: string[]; revision: number; revoked: boolean };
type IdentityStatus = { bots: Bot[]; catalog: Shared[]; connections: { userId: string; login: string; revoked: boolean }[]; delegation: Consent | null; sharedCurrent: boolean; owner: boolean; oauthConfigured: boolean; senderMode: string };
const capabilities = [['chat.send', 'Send messages'], ['chat.reply', 'Reply to messages'], ['chat.announce', 'Announcements'], ['message.delete', 'Delete messages'], ['user.timeout', 'Timeout users'], ['user.ban', 'Ban users'], ['user.unban', 'Unban users'], ['user.warn', 'Warn users']] as const;

export function BotIdentityPanel({ apiBaseUrl, operatorSecret }: { apiBaseUrl: string; operatorSecret: string }) {
  const { account, loading: accountLoading } = useAccount();
  const tenant = account.tenants.find((item) => item.id === account.activeTenantId);
  const canInspect = tenant?.role === 'owner' || tenant?.role === 'manager';
  const owner = tenant?.role === 'owner';
  const [status, setStatus] = useState<IdentityStatus>();
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState(() => new URLSearchParams(typeof window === 'undefined' ? '' : window.location.search).get('botAuth') === 'success' ? 'Bot authorization saved. Inspect the identity below before selecting it.' : '');
  const [consent, setConsent] = useState<string[]>(['chat.send', 'chat.reply']);
  const [confirmed, setConfirmed] = useState(false);
  const base = tenant ? `/api/tenants/${tenant.id}/bots` : '';
  const options = { explicitBaseUrl: apiBaseUrl, operatorSecret };
  const channel = status?.connections.find((item) => !item.revoked);
  useEffect(() => {
    let live = true;
    setStatus(undefined); setConfirmed(false); setConsent(['chat.send', 'chat.reply']); setError('');
    if (!base || !canInspect) return;
    setLoading(true);
    void requestJSON<IdentityStatus>(base, { ...options, method: 'GET', errorLabel: 'Bot identities' }).then((value) => { if (live) setStatus(value); }).catch((reason) => { if (live) setError(reason instanceof Error ? reason.message : 'Bot identities unavailable'); }).finally(() => { if (live) setLoading(false); });
    return () => { live = false; };
  }, [base, canInspect, apiBaseUrl, operatorSecret]);
  async function reload() {
    setConfirmed(false);
    setStatus(await requestJSON<IdentityStatus>(base, { ...options, method: 'GET', errorLabel: 'Bot identities' }));
    setConfirmed(false);
  }
  async function run(action: () => Promise<void>) {
    setBusy(true); setError(''); setMessage('');
    try { await action(); } catch (reason) { setError(reason instanceof Error ? reason.message : 'Bot identity action failed'); }
    finally { setBusy(false); }
  }
  const disabled = busy || loading;
  return <Panel id="bot-identity" kicker="Twitch authorization" title="Bot identity" description="Connect a bot account or approve a published shared bot for this workspace. Commands continue to use the broadcaster sender while the bot sender integration is being completed.">
    {accountLoading ? <p className="desk-note">Loading workspace…</p> : !tenant ? <Notice>Separate bot accounts are available in managed workspaces. <a href="/dashboard/account">Open account settings</a> to sign in or inspect your installation.</Notice> : !canInspect ? <Notice>The workspace owner or a manager can inspect bot connections. Only the owner can change the selected bot.</Notice> : <>
      {loading ? <p className="desk-note">Loading bot identities…</p> : null}
      {error ? <Notice tone="bad" role="alert">{error}</Notice> : null}
      {message ? <Notice tone="ok" role="status">{message}</Notice> : null}
      {!owner ? <Notice>Read-only access: ask the workspace owner to connect, select, or revoke a bot.</Notice> : null}
      <div className="flex flex-wrap gap-3">
        <button type="button" className="desk-btn" disabled={disabled} onClick={() => void run(reload)}>Reload identities</button>
        <button type="button" className="desk-btn desk-btn--primary" disabled={disabled || !owner || !status?.oauthConfigured} onClick={() => void run(async () => { const result = await requestJSON<{ authorizeUrl: string }>(`${base}/oauth`, { ...options, method: 'POST', body: {}, errorLabel: 'Bot authorization' }); window.location.assign(result.authorizeUrl); })}>Connect or reauthorize a bot</button>
      </div>
      {status && !status.oauthConfigured ? <Notice tone="warn">Twitch OAuth or the token vault is not configured for this installation.</Notice> : null}
      <p className="desk-note">Authorize the Twitch account you intend to use as the bot. Authorization saves its identity and permissions; selecting it below is a separate step.</p>
      <p className="desk-note">Broadcaster: {channel ? `${channel.login} (${channel.userId})` : 'Not connected. Connect Twitch in Integrations before selecting a bot.'}</p>
      <SubPanel title="Your bot accounts">
        {status?.bots.length === 0 ? <Empty>No bot accounts connected.</Empty> : null}
        <ul className="desk-rows">{status?.bots.map((bot) => <li className="desk-row" key={bot.id}>
          <div className="desk-row__line"><strong>{bot.login}</strong><span className="desk-note">Twitch ID {bot.userId}</span><Tag tone={bot.revoked ? 'warn' : bot.bound ? 'ok' : 'neutral'}>{bot.revoked ? 'Revoked' : bot.bound ? 'Selected' : 'Authorized'}</Tag></div>
          <p className="desk-note">Permissions: {bot.scopes.join(', ') || 'None retained'}</p>
          <div className="flex flex-wrap gap-2"><button type="button" className="desk-btn desk-btn--small" disabled={disabled || !owner || !channel || !confirmed || bot.revoked || bot.bound} onClick={() => void run(async () => { await requestJSON(`${base}/bind`, { ...options, method: 'POST', body: { botId: bot.id, channelUserId: channel?.userId }, errorLabel: 'Select bot', expectJson: false }); await reload(); setMessage('Bot selected. The current sender remains the broadcaster.'); })}>Select this bot</button>
          <button type="button" className="desk-btn desk-btn--small" disabled={disabled || !owner || !confirmed || bot.revoked} onClick={() => void run(async () => { await requestJSON(`${base}/${bot.id}`, { ...options, method: 'DELETE', errorLabel: 'Revoke bot', expectJson: false }); await reload(); setMessage('Bot authorization revoked and stored credentials removed.'); })}>Revoke authorization</button></div>
        </li>)}</ul>
      </SubPanel>
      <SubPanel title="Shared service bots">
        {status?.catalog.length === 0 ? <Empty>No shared bots have been published by the service administrator.</Empty> : null}
        {status?.delegation ? <p className="desk-note">Shared consent revision {status.delegation.revision}: {status.delegation.revoked ? 'Revoked' : status.sharedCurrent ? 'Current' : 'Needs new consent; the bot, publication, or broadcaster changed'}. Permissions: {status.delegation.capabilities.join(', ') || 'None'}.</p> : null}
        <fieldset disabled={disabled || !owner} className="grid gap-2"><legend className="desk-note mb-2">Shared bot permissions to approve</legend>{capabilities.map(([capability, label]) => <Check key={capability} label={label} checked={consent.includes(capability)} onChange={(checked) => { setConsent((current) => checked ? [...current, capability] : current.filter((item) => item !== capability)); setConfirmed(false); }} />)}</fieldset>
        <ul className="desk-rows">{status?.catalog.map((bot) => <li className="desk-row" key={bot.id}><div className="desk-row__line"><strong>{bot.login}</strong><span className="desk-note">Twitch ID {bot.userId}</span>{status.sharedCurrent && status.delegation?.botAccountId === bot.id ? <Tag tone="ok">Selected</Tag> : null}</div>
          <button type="button" className="desk-btn desk-btn--small" disabled={disabled || !owner || !channel || !confirmed || consent.length === 0} onClick={() => void run(async () => { await requestJSON(`${base}/shared`, { ...options, method: 'POST', body: { botId: bot.id, channelUserId: channel?.userId, grantRevision: bot.grantRevision, catalogRevision: bot.revision, revision: status.delegation?.revision || 0, capabilities: consent }, errorLabel: 'Approve shared bot' }); await reload(); setMessage('Shared bot consent saved. The current sender remains the broadcaster.'); })}>Approve this shared bot</button>
        </li>)}</ul>
        {status?.delegation && !status.delegation.revoked ? <button type="button" className="desk-btn" disabled={disabled || !owner || !confirmed} onClick={() => void run(async () => { await requestJSON(`${base}/shared`, { ...options, method: 'DELETE', body: { revision: status.delegation?.revision }, errorLabel: 'Revoke shared consent', expectJson: false }); await reload(); setMessage('Shared bot consent revoked for this workspace.'); })}>Revoke shared consent</button> : null}
      </SubPanel>
      {owner ? <fieldset disabled={disabled}><Check checked={confirmed} onChange={setConfirmed} label="I confirm the bot selection or revocation and the listed shared permissions. Selecting a bot replaces the previous own/shared binding." /></fieldset> : null}
      <p className="desk-note">Saved permissions do not establish Twitch moderator status. Provider role checks and bot sending will be verified when the sender integration is enabled.</p>
    </>}
  </Panel>;
}
