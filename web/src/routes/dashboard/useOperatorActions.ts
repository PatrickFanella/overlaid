import { useRef, useState } from 'react';
import {
  clearSessionLog,
  createSessionLogEntry,
  loadSessionLog,
  prependSessionLogEntry,
  saveSessionLog,
  type SessionLogEntry,
} from '../../lib/sessionLog';

// Runs one operator action at a time and records each start, success, and
// failure in the browser session log.
export function useOperatorActions() {
  const [sessionLog, setSessionLog] = useState<SessionLogEntry[]>(() => loadSessionLog());
  const sessionLogRef = useRef(sessionLog);
  const [runningAction, setRunningAction] = useState<string | null>(null);
  const actionPending = useRef(false);

  function recordLog(label: string, status: SessionLogEntry['status'], message: string) {
    const next = prependSessionLogEntry(sessionLogRef.current, createSessionLogEntry(label, status, message));
    sessionLogRef.current = next;
    setSessionLog(next);
    saveSessionLog(next);
  }

  function runAction(label: string, action: () => Promise<string | void>) {
    if (actionPending.current) return;
    actionPending.current = true;
    const startedAt = Date.now();
    setRunningAction(label);
    recordLog(label, 'running', 'Action started.');
    void Promise.resolve().then(action)
      .then((message) => {
        recordLog(label, 'success', `${message || 'Action completed.'} (${formatDuration(Date.now() - startedAt)})`);
      })
      .catch((error) => {
        const detail = error instanceof Error ? error.message : 'Action failed.';
        recordLog(label, 'error', `${detail} (${formatDuration(Date.now() - startedAt)})`);
      })
      .finally(() => {
        actionPending.current = false;
        setRunningAction(null);
      });
  }

  function clearLog() {
    sessionLogRef.current = [];
    clearSessionLog();
    setSessionLog([]);
  }

  return { sessionLog, runAction, clearLog, isRunning: runningAction !== null };
}

function formatDuration(ms: number) {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}
