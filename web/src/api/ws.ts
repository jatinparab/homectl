export function buildWebSocketUrl(path: string): string {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${window.location.host}${path}`;
}

export function openStreamSocket(path: string): WebSocket {
  // path is like /api/v1/apps/foo/logs?node=bar
  // → rewrite to   /api/v1/ws/apps/foo/logs?node=bar  (insert /ws after /api/v1)
  const streamPath = path.replace(/^\/api\/v1\//, '/api/v1/ws/');
  return new WebSocket(buildWebSocketUrl(streamPath));
}

export function decodeStreamMessage(raw: unknown): string | null {
  if (typeof raw !== "string") {
    return null;
  }
  try {
    const parsed = JSON.parse(raw) as { line?: unknown };
    if (typeof parsed.line === "string") {
      return parsed.line;
    }
  } catch {
    return raw;
  }
  return null;
}
