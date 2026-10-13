import { getOverlayApiBaseUrl } from './eventStream';
import { accountHeaders } from './accountContext';

type CredentialOptions = {
  operatorSecret?: string;
  webhookSecret?: string;
};

type RequestJSONOptions = CredentialOptions & {
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  body?: Record<string, unknown>;
  errorLabel: string;
  includeDetail?: boolean;
  expectJson?: boolean;
};

export async function requestJSON(path: string, options: RequestJSONOptions & { explicitBaseUrl?: string; expectJson: false }): Promise<void>;
export async function requestJSON<T>(path: string, options: RequestJSONOptions & { explicitBaseUrl?: string; expectJson?: true }): Promise<T>;
export async function requestJSON<T>(path: string, options: RequestJSONOptions & { explicitBaseUrl?: string }) {
  const headers = operatorHeaders(options.operatorSecret);
  if (options.webhookSecret?.trim()) {
    headers['X-Overlay-Webhook-Secret'] = options.webhookSecret.trim();
  }
  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }

  const response = await fetch(new URL(path, getOverlayApiBaseUrl(options.explicitBaseUrl)).toString(), {
    method: options.method,
    credentials: 'include',
    headers,
    ...(options.body !== undefined ? { body: JSON.stringify(options.body) } : {}),
  });

  if (!response.ok) {
    const detail = options.includeDetail === false ? '' : await response.text().catch(() => '');
    throw new Error(`${options.errorLabel} failed: ${response.status}${detail.trim() ? ` — ${detail.trim()}` : ''}`);
  }

  if (options.expectJson === false) {
    return;
  }

  try {
    return (await response.json()) as T;
  } catch {
    throw new Error(`${options.errorLabel} failed: invalid JSON response`);
  }
}

function operatorHeaders(operatorSecret?: string) {
  const headers: Record<string, string> = { Accept: 'application/json', ...accountHeaders() };
  if (operatorSecret?.trim()) {
    headers['X-Overlay-Operator-Secret'] = operatorSecret.trim();
  }
  return headers;
}
