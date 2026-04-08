export function buildWebSocketUrl(path: string): string {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${window.location.host}${path}`;
}

export function openStreamSocket(path: string): WebSocket {
  const streamPath = `/api/v1/ws/stream?path=${encodeURIComponent(path)}`;
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
