import type {
  BotAuditResponse,
  BotStatus,
} from '../../../lib/api';

export type BotPageState = {
  status: BotStatus | null;
  audit: BotAuditResponse | null;
  loading: boolean;
  error: string;
};

export type CommandDraft = {
  id?: string;
  name: string;
  response: string;
  action: 'ticker' | 'alert' | 'newsroom_submit' | 'newsroom_capture' | 'newsroom_current' | 'newsroom_question' | 'newsroom_raffle';
  responseMode: 'overlay' | 'chat' | 'both';
  roleRequired: 'viewer' | 'supporter' | 'moderator' | 'broadcaster' | 'operator' | 'system';
  cooldownSeconds: number;
  enabled: boolean;
};

export type TimerDraft = {
  id?: string;
  name: string;
  intervalSeconds: number;
  response: string;
  action: 'ticker' | 'alert';
  enabled: boolean;
};

export type CounterDraft = { name: string; value: number };
export type QuoteDraft = { text: string; author: string };
export type APIVariableDraft = {
  id?: string;
  name: string;
  enabled: boolean;
  url: string;
  responseSelector: string;
  fallback: string;
  cacheTtlSeconds: number;
  timeoutMs: number;
};

export type ModerationDraft = {
  id?: string;
  exemptRole: string;
  timeoutSeconds: number;
  name: string;
  enabled: boolean;
  enforce: boolean;
  pattern: string;
  matchTarget: 'message' | 'username' | 'both';
  caseSensitive: boolean;
  regex: boolean;
  action: 'flag' | 'delete' | 'timeout' | 'ban';
  reason: string;
};

export type ModerationTestDraft = { username: string; message: string; role: string };

export type ModerationActionDraft = {
  action: 'delete' | 'timeout' | 'ban' | 'unban';
  targetUserId: string;
  targetUserLogin: string;
  messageId: string;
  durationSeconds: number;
  reason: string;
};

export type NukeDraft = {
  pattern: string;
  regex: boolean;
  caseSensitive: boolean;
  lookbackMinutes: number;
  limit: number;
  action: 'delete' | 'timeout';
  durationSeconds: number;
  confirm: boolean;
  radiationMinutes: number;
};

export const defaultTimerDraft: TimerDraft = { name: '', intervalSeconds: 300, response: '', action: 'ticker', enabled: true };
export const defaultCommandDraft: CommandDraft = { name: '', response: '', action: 'ticker', responseMode: 'overlay', roleRequired: 'viewer', cooldownSeconds: 10, enabled: true };
export const defaultCounterDraft: CounterDraft = { name: '', value: 0 };
export const defaultQuoteDraft: QuoteDraft = { text: '', author: '' };
export const defaultAPIVariableDraft: APIVariableDraft = { name: '', enabled: true, url: '', responseSelector: '', fallback: '', cacheTtlSeconds: 60, timeoutMs: 1500 };
export const defaultModerationDraft: ModerationDraft = { exemptRole: 'moderator', timeoutSeconds: 60, name: '', enabled: true, enforce: false, pattern: '', matchTarget: 'message', caseSensitive: false, regex: false, action: 'flag', reason: '' };
export const defaultModerationTest: ModerationTestDraft = { role: 'viewer', username: '', message: '' };
export const defaultModerationAction: ModerationActionDraft = { action: 'timeout', targetUserId: '', targetUserLogin: '', messageId: '', durationSeconds: 60, reason: '' };
export const defaultNukeDraft: NukeDraft = { pattern: '', regex: false, caseSensitive: false, lookbackMinutes: 30, limit: 100, action: 'delete', durationSeconds: 60, confirm: false, radiationMinutes: 0 };
