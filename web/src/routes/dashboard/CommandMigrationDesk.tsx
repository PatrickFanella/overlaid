import { useEffect, useRef, useState } from 'react';
import type { VersionScope } from '../../lib/automationVersions';
import { listCommandMigration, type CommandMigrationItem } from '../../lib/commandMigration';
import { TemplateConversionDesk } from './TemplateConversionDesk';
import { Empty, KeyValues, Notice, Panel } from './ui';

export function CommandMigrationDesk({ scope }: { scope: VersionScope }) {
  const [items, setItems] = useState<CommandMigrationItem[] | null>(null);
  const [selected, setSelected] = useState<CommandMigrationItem | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const request = useRef<AbortController | null>(null);
  const mounted = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; request.current?.abort(); }; }, []);

  async function load() {
    if (request.current) return;
    const current = new AbortController(); request.current = current;
    setSelected(null); setItems(null); setError(''); setBusy(true);
    const timeout = window.setTimeout(() => current.abort(), 15000);
    try {
      const next = await listCommandMigration(scope, current.signal);
      if (mounted.current && request.current === current && !current.signal.aborted) setItems(next);
    } catch {
      if (mounted.current && request.current === current) setError(current.signal.aborted ? 'Command catalogue read canceled or timed out. Load it again to continue.' : 'Saved commands could not be read. The catalogue must contain at most 1,000 valid saved commands.');
    } finally {
      window.clearTimeout(timeout);
      if (request.current === current) { request.current = null; if (mounted.current) setBusy(false); }
    }
  }

  return <Panel title="Saved command migration preview" description="Inspect the saved Chat registry and compare a selected response with syntax 2. This is a compatibility preview; command editing and execution stay in Chat.">
    <button className="desk-btn w-fit" disabled={busy} onClick={() => void load()}>Load saved commands</button>
    {busy ? <p role="status" className="mt-3">Reading saved command snapshots…</p> : null}
    {error ? <div className="mt-3"><Notice role="alert" tone="bad">{error}</Notice></div> : null}
    {items ? items.length ? <ul aria-label="Saved command catalogue" className="mt-3 grid max-h-96 gap-2 overflow-y-auto sm:grid-cols-2">{items.map(item => <li key={item.id} className="desk-sub min-w-0 break-all"><button className="desk-btn max-w-full whitespace-normal break-all" aria-pressed={selected?.id === item.id} onClick={() => setSelected(item)}>!{item.name}</button><p className="mt-2 text-sm">{item.action} / {item.responseMode} / {item.enabled ? 'enabled' : 'disabled'}</p></li>)}</ul> : <div className="mt-3"><Empty>No commands are saved in this workspace.</Empty></div> : !busy && !error ? <div className="mt-3"><Empty>Load the catalogue to choose a saved command. Separate saved aliases keep their own identity.</Empty></div> : null}
    {selected ? <section aria-label="Selected command snapshot" className="mt-4 grid min-w-0 gap-3">
      <div className="break-all"><KeyValues items={[
        ['Command', `!${selected.name}`], ['Saved ID', selected.id], ['Snapshot', selected.snapshot], ['Action', selected.action], ['Response mode', selected.responseMode], ['Required role', selected.roleRequired], ['Cooldown seconds', String(selected.cooldownSeconds)], ['State', selected.enabled ? 'enabled' : 'disabled'],
      ]} /></div>
      {selected.kind === 'editorial' ? <Notice>Newsroom actions need a separate editorial migration contract. Their saved response field is not converted here.</Notice> : <><Notice>Only the response text is compared. Equivalent effects, roles, cooldowns and source routing still need migration review before this command can use the engine.</Notice><TemplateConversionDesk key={`${selected.id}/${selected.snapshot}`} scope={scope} command={selected} /></>}
    </section> : null}
  </Panel>;
}
