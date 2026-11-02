import { apiRoute } from './util';

export type AnalyticsOutcome = 'success' | 'error';
export type OverrideControl = 'switch' | 'menu' | 'editor';

export function valueKind(value: unknown): string {
  if (value === null) {
    return 'null';
  }
  if (Array.isArray(value)) {
    return 'array';
  }
  switch (typeof value) {
    case 'boolean':
    case 'number':
    case 'string':
      return typeof value;
    case 'object':
      return 'object';
    default:
      return 'object';
  }
}

export function pageNameFromPath(pathname: string): string | null {
  if (pathname === '/ui/flags') {
    return 'flags';
  }
  if (pathname === '/ui/events') {
    return 'events';
  }
  if (pathname === '/ui/debug-sessions') {
    return 'debug-sessions';
  }
  if (/^\/ui\/debug-sessions\/[^/]+\/events$/.test(pathname)) {
    return 'debug-session-events';
  }
  return null;
}

export function sendUiAnalytics(
  event: string,
  properties: Record<string, string | boolean> = {},
) {
  fetch(apiRoute('/ui-analytics'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ event, properties }),
  }).catch(() => {
    // The analytics post failed. The screen action still stands.
  });
}
