import { tenantStorageKey } from './accountContext';
export type SessionLogEntry = {
  id: string;
  label: string;
  status: 'running' | 'success' | 'error';
  message: string;
  createdAt: string;
};

const SESSION_LOG_KEY = 'overlaid.sessionLog.v1';
const MAX_ENTRIES = 24;

export function loadSessionLog(): SessionLogEntry[] {
  if (typeof window === 'undefined') {
    return [];
  }

  const raw = window.sessionStorage.getItem(tenantStorageKey(SESSION_LOG_KEY));
  if (!raw) {
    return [];
  }

  try {
    const parsed = JSON.parse(raw) as SessionLogEntry[];
    return Array.isArray(parsed) ? parsed.slice(0, MAX_ENTRIES) : [];
  } catch {
    return [];
  }
}

export function saveSessionLog(entries: SessionLogEntry[]) {
  if (typeof window === 'undefined') {
    return;
  }

  window.sessionStorage.setItem(tenantStorageKey(SESSION_LOG_KEY), JSON.stringify(entries.slice(0, MAX_ENTRIES)));
}

export function clearSessionLog() {
  if (typeof window === 'undefined') {
    return;
  }

  window.sessionStorage.removeItem(tenantStorageKey(SESSION_LOG_KEY));
}

export function createSessionLogEntry(label: string, status: SessionLogEntry['status'], message: string): SessionLogEntry {
  return {
    id: createLogId(),
    label,
    status,
    message,
    createdAt: new Date().toISOString(),
  };
}

export function prependSessionLogEntry(entries: SessionLogEntry[], entry: SessionLogEntry) {
  return [entry, ...entries].slice(0, MAX_ENTRIES);
}

function createLogId() {
  if (typeof globalThis.crypto !== 'undefined' && typeof globalThis.crypto.randomUUID === 'function') {
    return globalThis.crypto.randomUUID();
  }

  return `log-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}
