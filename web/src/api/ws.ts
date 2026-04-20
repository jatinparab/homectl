function normalizeBasePath(pathname: string): string {
  if (!pathname || pathname === "/") {
    return "";
  }
  return pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
}

function normalizeTarget(target: string, protocol: string, path: string): string {
  const trimmed = target.trim();
  if (trimmed === "") {
    return `${protocol}//${window.location.host}${path}`;
  }

  if (/^wss?:\/\//i.test(trimmed) || /^https?:\/\//i.test(trimmed)) {
    const url = new URL(trimmed);
    const wsProtocol = url.protocol === "https:" ? "wss:" : url.protocol === "http:" ? "ws:" : url.protocol;
    const basePath = normalizeBasePath(url.pathname);
    return `${wsProtocol}//${url.host}${basePath}${path}`;
  }

  if (trimmed.startsWith("/")) {
    const basePath = normalizeBasePath(trimmed);
    return `${protocol}//${window.location.host}${basePath}${path}`;
  }

  return `${protocol}//${trimmed}${path}`;
}

export function buildWebSocketUrl(path: string): string {
  return buildWebSocketUrlCandidates(path)[0];
}

export function buildWebSocketUrlCandidates(path: string): string[] {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const rawTargets: string[] = [window.location.host];
  if (import.meta.env.VITE_WS_TARGET) {
    rawTargets.push(import.meta.env.VITE_WS_TARGET);
  }

  const urls: string[] = [];
  const seen = new Set<string>();
  for (const target of rawTargets) {
    const url = normalizeTarget(target, protocol, path);
    if (!seen.has(url)) {
      seen.add(url);
      urls.push(url);
    }
  }
  return urls;
}
