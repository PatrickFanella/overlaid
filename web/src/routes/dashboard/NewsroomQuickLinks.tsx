import { useState } from 'react';
import type { NewsroomDocument } from '../../lib/newsroom';
import { Field, Panel } from './ui';
type Command = (action: string, fields?: Record<string, unknown>) => Promise<boolean>;
export function NewsroomQuickLinks({ doc, command, busy }: { doc: NewsroomDocument; command: Command; busy: boolean }) {
  const [name, setName] = useState(''); const [url, setURL] = useState('');
  return <Panel title="Quick sources" description="Keep recurring sources nearby; each button submits the saved source to triage."><form className="flex flex-wrap items-end gap-3" onSubmit={async (e) => { e.preventDefault(); if (await command('quicklink-save', { name, url })) { setName(''); setURL(''); } }}><Field label="Source name"><input className="desk-input" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field><Field label="Source URL"><input className="desk-input" type="url" required value={url} onChange={(e) => setURL(e.target.value)} /></Field><button className="desk-btn" disabled={busy}>Save quick source</button></form>{(doc.quickLinks ?? []).map((link) => <div key={link.id} className="desk-newsroom-row"><strong>{link.name}</strong><span className="desk-newsroom-url">{link.url}</span><button className="desk-btn desk-btn--small desk-btn--ghost" disabled={busy} onClick={() => { void command('quicklink-delete', { id: link.id }); }}>Remove quick source</button></div>)}</Panel>;
}
