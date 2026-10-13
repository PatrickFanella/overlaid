import { useEffect, useState } from 'react';
import { requestJSON } from '../../lib/apiRequest';
import { saveAccountContext } from '../../lib/accountContext';
import { DashboardShell } from './DashboardShell';
import { loginURL, signOut, useAccount } from './accountSession';

const limitNames: Record<string, string> = {apiVariables:'API variables',commands:'Commands',httpCallsPerHour:'HTTP calls per hour',mediaBytes:'Media storage, bytes',storedEvents:'Stored events',timers:'Timers'};

export function DashboardAccountPage() {
  return <DashboardShell activePath="/dashboard/account"><AccountWorkspace /></DashboardShell>;
}

function AccountWorkspace() {
  const { account, loading, refresh } = useAccount();
  const tenant = account.tenants.find((item) => item.id === account.activeTenantId);
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [memberId, setMemberId] = useState('');
  const [role, setRole] = useState('editor');
  const [members, setMembers] = useState<Array<{ accountId: string; displayName: string; role: string }>>([]);
  const [plan, setPlan] = useState<{ plan: string; limits: Record<string, number> }>();
  const [retention, setRetention] = useState(30);
  const [viewerId, setViewerId] = useState('');
  const [viewerLogin, setViewerLogin] = useState('');
  const [confirm, setConfirm] = useState(false);
  const [disconnectConfirm, setDisconnectConfirm] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState(false);
  const [viewerConfirm, setViewerConfirm] = useState(false);
  const [transferConfirm, setTransferConfirm] = useState(false);
  const [credential, setCredential] = useState<{ purpose: string; token: string }>();
  const manage = tenant?.role === 'owner' || tenant?.role === 'manager';
  const owner = tenant?.role === 'owner';
  const base = tenant ? `/api/tenants/${tenant.id}` : '';
  useEffect(() => {
    if (!tenant) return;
    setRetention(tenant.retentionDays);
    void requestJSON<{ plan: string; limits: Record<string, number> }>(`${base}/plan`, { method: 'GET', errorLabel: 'Plan' }).then(setPlan).catch(() => setPlan(undefined));
    if (manage) void requestJSON<{ members: typeof members }>(`${base}/members`, { method: 'GET', errorLabel: 'Members' }).then((value) => setMembers(value.members)).catch(() => setMembers([]));
  }, [base, manage, tenant?.retentionDays]);

  async function run(action: () => Promise<void>) {
    setBusy(true); setMessage('');
    try { await action(); } catch (error) { setMessage(error instanceof Error ? error.message : 'Operation failed'); }
    finally { setBusy(false); }
  }
  async function select(id: string) {
    await requestJSON('/api/account/select-tenant', { method: 'POST', body: { tenantId: id }, errorLabel: 'Select tenant' });
    saveAccountContext({ deploymentMode: account.deploymentMode, tenantId: id, csrf: account.csrfToken });
    window.location.href = '/dashboard';
  }
  return <>
    <h1 className="desk-title">Account and workspace</h1>
    {message ? <p role="status" className="desk-note">{message}</p> : null}
    {loading ? <p>Loading account…</p> : !account.authenticated ? <section className="desk-panel p-5"><h2 className="desk-panel__title mb-3">Sign in</h2><p>Use your Twitch account to manage your workspaces.</p><a className="desk-btn" href={loginURL()}>Sign in with Twitch</a><p>For a private installation, enter the operator secret in <a href="/dashboard/settings">Settings</a>.</p><p><a href="/privacy">Privacy policy</a> · <a href="/terms">Terms of service</a></p></section> : <>
      <section className="desk-panel p-5">
        <h2 className="desk-panel__title mb-3">Your workspaces</h2><p className="desk-note">Account ID: <code>{account.accountId}</code>. Share this ID with a workspace owner to receive access.</p>
        {account.tenants.map((item) => <p key={item.id}><strong>{item.name}</strong> · {item.role} · {item.plan} <button className="desk-btn" disabled={busy || item.id === account.activeTenantId} onClick={() => void run(() => select(item.id))}>{item.id === account.activeTenantId ? 'Selected' : 'Switch workspace'}</button></p>)}
        {account.deploymentMode === 'hosted' ? <form className="grid gap-3 mt-4" onSubmit={(event) => { event.preventDefault(); void run(async () => { const created = await requestJSON<{ id: string }>('/api/tenants', { method: 'POST', body: { name, slug }, errorLabel: 'Create workspace' }); await select(created.id); }); }}>
          <label>Workspace name<input className="desk-input" value={name} onChange={(event) => setName(event.target.value)} maxLength={80} required disabled={busy} /></label>
          <label>Workspace slug<input className="desk-input" value={slug} onChange={(event) => setSlug(event.target.value)} pattern="[a-z0-9][a-z0-9-]{1,62}" required disabled={busy} /></label>
          <button className="desk-btn" disabled={busy}>Create workspace</button>
        </form> : null}
        {account.deploymentMode === 'hosted' ? <div className="flex flex-wrap gap-2 mt-4"><button className="desk-btn" disabled={busy} onClick={() => void run(signOut)}>Sign out</button><button className="desk-btn" disabled={busy} onClick={() => void run(async () => { await requestJSON('/api/account/revoke-sessions', { method: 'POST', body: {}, errorLabel: 'Revoke sessions', expectJson: false }); window.location.href='/dashboard/account'; })}>Sign out all sessions</button></div> : null}
      </section>
      {tenant ? <>
        <section className="desk-panel p-5"><h2 className="desk-panel__title mb-3">Plan and retention</h2><p>{tenant.name} uses the {tenant.plan} plan. Limits update when the operator changes the assignment. A downgrade preserves saved configuration and pauses execution above the new limits.</p>
          {plan ? <dl>{Object.entries(plan.limits).map(([key, value]) => <div key={key} className="flex justify-between gap-4 py-1"><dt> {limitNames[key] || key}</dt><dd>{value === -1 ? 'Unlimited commercial limit' : value}</dd></div>)}</dl> : null}
          {owner ? <form className="grid gap-3 mt-4" onSubmit={(event) => { event.preventDefault(); void run(async () => { await requestJSON(`${base}/retention`, { method: 'PUT', body: { days: retention }, errorLabel: 'Retention', expectJson: false }); await refresh(); setMessage('Retention saved. Expiry runs every minute.'); }); }}><label>Event and audit retention, days<input className="desk-input" type="number" min={1} max={365} value={retention} onChange={(event) => setRetention(Number(event.target.value))} disabled={busy} /></label><button className="desk-btn" disabled={busy}>Save retention</button></form> : null}
        </section>
        {manage ? <section className="desk-panel p-5"><h2 className="desk-panel__title mb-3">Members</h2>
          {members.map((member) => <p key={member.accountId}>{member.displayName} · {member.role} · <code>{member.accountId}</code>{member.role !== 'owner' && member.accountId !== account.accountId ? <button className="desk-btn" disabled={busy || tenant.role === 'manager' && member.role === 'manager'} onClick={() => void run(async () => { await requestJSON(`${base}/members/${member.accountId}`, { method: 'DELETE', errorLabel: 'Remove member', expectJson: false }); setMembers(members.filter((item) => item.accountId !== member.accountId)); setMessage('Member removed.'); })}>Remove</button> : null}</p>)}
          <form className="grid gap-3 mt-4" onSubmit={(event) => { event.preventDefault(); void run(async () => { await requestJSON(`${base}/members`, { method: 'POST', body: { accountId: memberId, role }, errorLabel: 'Save member', expectJson: false }); const result = await requestJSON<{ members: typeof members }>(`${base}/members`, { method: 'GET', errorLabel: 'Members' }); setMembers(result.members); setMessage('Membership saved and audited.'); }); }}>
            <label>Account ID<input className="desk-input" value={memberId} onChange={(event) => setMemberId(event.target.value)} required disabled={busy} /></label>
            <label>Role<select className="desk-input" value={role} onChange={(event) => setRole(event.target.value)} disabled={busy}>{(owner ? ['manager', 'editor', 'moderator'] : ['editor', 'moderator']).map((value) => <option key={value}>{value}</option>)}</select></label><button className="desk-btn" disabled={busy}>Invite or update member</button>
          </form>
          {owner ? <><label><input type="checkbox" checked={transferConfirm} onChange={(event) => setTransferConfirm(event.target.checked)} disabled={busy} /> Transfer ownership to the entered account ID; I will become a manager</label><button className="desk-btn" disabled={busy || !transferConfirm || !memberId} onClick={() => void run(async () => { await requestJSON(`${base}/owner`, { method: 'POST', body: { accountId: memberId, confirm: 'transfer-ownership' }, errorLabel: 'Transfer ownership', expectJson: false }); setTransferConfirm(false); await refresh(); setMessage('Ownership transferred and audited.'); })}>Transfer ownership to an existing member</button></> : null}
        </section> : null}
        {manage ? <section className="desk-panel p-5"><h2 className="desk-panel__title mb-3">Overlay and webhook credentials</h2><p>Rotation invalidates old URLs and stops active streams. The new credential is shown once; copy it to your OBS or webhook configuration.</p>
          <label><input type="checkbox" checked={confirm} onChange={(event) => setConfirm(event.target.checked)} disabled={busy} /> Confirm credential rotation; old source URLs will stop working</label>
          {['overlay', 'webhook'].map((purpose) => <button className="desk-btn" key={purpose} disabled={busy || !confirm} onClick={() => void run(async () => { const value = await requestJSON<{ purpose: string; token: string }>(`${base}/credentials/${purpose}`, { method: 'POST', body: { confirm: 'rotate' }, errorLabel: 'Rotate credential' }); setCredential(value); setConfirm(false); setMessage(`${purpose} credential rotated. Copy the new value before leaving this page.`); })}>Rotate {purpose} credential</button>)}
          {credential ? <label>New {credential.purpose} credential<input className="desk-input" readOnly value={credential.token} onFocus={(event) => event.target.select()} /></label> : null}
          <label><input type="checkbox" checked={disconnectConfirm} onChange={(event) => setDisconnectConfirm(event.target.checked)} disabled={busy} /> Disconnect Twitch and remove its stored tokens</label><button className="desk-btn" disabled={busy || !disconnectConfirm} onClick={() => void run(async () => { await requestJSON(`${base}/connections/twitch`, { method: 'DELETE', errorLabel: 'Disconnect Twitch', expectJson: false }); setDisconnectConfirm(false); setMessage('Twitch disconnected; stored tokens were removed.'); })}>Disconnect Twitch</button>
        </section> : null}
        {owner ? <section className="desk-panel p-5"><h2 className="desk-panel__title mb-3">Export and deletion</h2>
          <button className="desk-btn" disabled={busy} onClick={() => void run(async () => { const value = await requestJSON<Record<string, unknown>>(`${base}/export`, { method: 'GET', errorLabel: 'Export' }); const url = URL.createObjectURL(new Blob([JSON.stringify(value, null, 2)], { type: 'application/json' })); const link = document.createElement('a'); link.href = url; link.download = 'tenant-export.json'; link.click(); URL.revokeObjectURL(url); setMessage('Workspace export downloaded without credentials.'); })}>Export workspace</button>
          <label><input type="checkbox" checked={viewerConfirm} onChange={(event) => setViewerConfirm(event.target.checked)} disabled={busy} /> Delete matching viewer records and clear derived overlay state</label><form className="grid gap-3 mt-4" onSubmit={(event) => { event.preventDefault(); void run(async () => { await requestJSON(`${base}/viewers`, { method: 'DELETE', body: { userId: viewerId, login: viewerLogin, confirm: 'delete-viewer' }, errorLabel: 'Viewer deletion', expectJson: false }); setViewerConfirm(false); setMessage('Viewer records deleted and derived retained state cleared.'); }); }}><label>Viewer Twitch user ID<input className="desk-input" value={viewerId} onChange={(event) => setViewerId(event.target.value)} required disabled={busy} /></label><label>Viewer login<input className="desk-input" value={viewerLogin} onChange={(event) => setViewerLogin(event.target.value)} disabled={busy} /></label><button className="desk-btn" disabled={busy || !viewerConfirm}>Delete viewer records</button></form>
          {account.deploymentMode === 'hosted' ? <><label><input type="checkbox" checked={deleteConfirm} onChange={(event) => setDeleteConfirm(event.target.checked)} disabled={busy} /> Permanently delete this workspace and revoke its credentials and active sessions</label><button className="desk-btn" disabled={busy || !deleteConfirm} onClick={() => void run(async () => { await requestJSON(base, { method: 'DELETE', body: { confirm: tenant.id }, errorLabel: 'Delete workspace', expectJson: false }); setDeleteConfirm(false); await refresh(); setMessage('Workspace deleted; its credentials and active sessions were revoked.'); })}>Delete this workspace</button></> : null}
        </section> : null}
      </> : null}
    </>}
    <p><a href="/privacy">Privacy policy</a> · <a href="/terms">Terms of service</a> · <a href="/status">Service status</a></p>
  </>;
}
