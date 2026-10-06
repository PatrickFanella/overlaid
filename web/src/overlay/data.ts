// Loose readers for event `data` payloads, which arrive as untyped JSON.

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function asString(value: unknown) {
  return typeof value === 'string' ? value.trim() : '';
}

export function asNumber(value: unknown) {
  if (typeof value === 'number') {
    return value;
  }
  if (typeof value === 'string' && value.trim()) {
    return Number(value);
  }
  return Number.NaN;
}

export function asStringArray(value: unknown, limit = 5) {
  if (!Array.isArray(value)) {
    return [];
  }
  return value
    .filter((item): item is string => typeof item === 'string' && item.trim().length > 0)
    .map((item) => item.trim())
    .slice(0, limit);
}

export function formatClock(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

// Splits a headline so its last word can sit on a green slab.
export function splitLastWord(text: string) {
  const trimmed = text.trim();
  const index = trimmed.lastIndexOf(' ');
  if (index < 0) {
    return { lead: '', last: trimmed };
  }
  return { lead: trimmed.slice(0, index), last: trimmed.slice(index + 1) };
}
