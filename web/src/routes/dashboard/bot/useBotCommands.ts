import { useEffect, useRef, useState } from 'react';
import {
  deleteBotCommand, fetchBotCommands, saveBotCommand, setBotCommandEnabled,
  type BotCommand, type BotCommandsResponse,
} from '../../../lib/api';
import { defaultCommandDraft, type CommandDraft } from './types';

type CommandOperation = { action: 'save' | 'toggle' | 'delete'; id?: string };
type CommandChange = { message: string; command?: BotCommand; deletedId?: string };

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : 'Command operation unavailable';
}

export function useBotCommands(apiBaseUrl: string, operatorSecret: string, onChange: () => Promise<void>) {
  const [commands, setCommands] = useState<BotCommandsResponse | null>(null);
  const [draft, setDraft] = useState<CommandDraft>(defaultCommandDraft);
  const [pending, setPending] = useState<CommandOperation | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [result, setResult] = useState('');
  const generation = useRef(0);
  const busy = useRef(false);

  useEffect(() => {
    const current = ++generation.current;
    busy.current = false;
    setPending(null);
    setLoading(true);
    setCommands(null);
    setDraft(defaultCommandDraft);
    setError('');
    setResult('');
    void fetchBotCommands(apiBaseUrl, operatorSecret).then(
      (loaded) => {
        if (generation.current === current) { setCommands(loaded); setLoading(false); }
      },
      (failure: unknown) => {
        if (generation.current === current) {
          setCommands({ commands: [], loadError: errorMessage(failure) });
          setLoading(false);
        }
      },
    );
    return () => { generation.current += 1; };
  }, [apiBaseUrl, operatorSecret]);

  async function perform(operation: CommandOperation, task: () => Promise<CommandChange>) {
    if (loading || busy.current) return;
    busy.current = true;
    const current = generation.current;
    setPending(operation);
    setError('');
    setResult('');
    try {
      const change = await task();
      if (generation.current !== current) return;
      if (change.command) {
        const saved = change.command;
        setCommands((previous) => ({
          ...previous,
          commands: [...(previous?.commands ?? []).filter((command) => command.id !== saved.id), saved].sort((left, right) => left.name.localeCompare(right.name)),
        }));
        if (operation.action === 'toggle') {
          setDraft((previous) => previous.id === saved.id ? { ...previous, enabled: saved.enabled } : previous);
        }
      }
      if (change.deletedId) {
        setCommands((previous) => ({ ...previous, commands: (previous?.commands ?? []).filter((command) => command.id !== change.deletedId) }));
        setDraft((previous) => previous.id === change.deletedId ? defaultCommandDraft : previous);
      }
      if (operation.action === 'save') setDraft(defaultCommandDraft);
      setResult(change.message);
      try { await onChange(); }
      catch (failure) {
        if (generation.current === current) setError(`Command operation succeeded. Status/audit refresh failed: ${errorMessage(failure)}`);
      }
    } catch (failure) {
      if (generation.current === current) setError(errorMessage(failure));
    } finally {
      if (generation.current === current) { busy.current = false; setPending(null); }
    }
  }

  function onEdit(command: BotCommand) {
    if (busy.current) return;
    setDraft({ id: command.id, name: command.name, enabled: command.enabled, response: command.response,
      action: command.action as CommandDraft['action'], responseMode: (command.responseMode || 'overlay') as CommandDraft['responseMode'],
      roleRequired: command.roleRequired as CommandDraft['roleRequired'], cooldownSeconds: command.cooldownSeconds });
    setError('');
    setResult('');
  }

  return {
    commands, draft, loading, error, result, pending,
    onDraftChange: setDraft,
    onEdit,
    onCancel: () => { if (!busy.current) { setDraft(defaultCommandDraft); setError(''); } },
    onSave: () => perform({ action: 'save', id: draft.id }, async () => {
      const command = await saveBotCommand(draft, apiBaseUrl, operatorSecret);
      return { command, message: `${draft.id ? 'Updated' : 'Created'} ${command.name}.` };
    }),
    onToggle: (command: BotCommand) => perform({ action: 'toggle', id: command.id }, async () => {
      const updated = await setBotCommandEnabled(command.id, !command.enabled, apiBaseUrl, operatorSecret);
      return { command: updated, message: `${updated.enabled ? 'Enabled' : 'Paused'} ${updated.name}.` };
    }),
    onDelete: (command: BotCommand) => perform({ action: 'delete', id: command.id }, async () => {
      await deleteBotCommand(command.id, apiBaseUrl, operatorSecret);
      return { deletedId: command.id, message: `Deleted ${command.name}.` };
    }),
  };
}

export type BotCommandsState = ReturnType<typeof useBotCommands>;
