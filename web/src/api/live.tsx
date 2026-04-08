import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type {
  Agent,
  AppStatus,
  GatewayInfo,
  NodeContainers,
  RepoStatus,
  StorageStatus,
  UpcomingBackup,
  UpcomingPrune,
} from "./types";
import { buildWebSocketUrl } from "./ws";

interface LiveStatePayload {
  apps: AppStatus[];
  agents: Agent[];
  system_log_sources: string[];
  containers: NodeContainers[];
  storage: StorageStatus;
  upcoming_backup: UpcomingBackup | null;
  upcoming_prune: UpcomingPrune | null;
  repo_status: RepoStatus;
  gateway: GatewayInfo;
}

// Stream subscription callback — line is "" when done is true.
export type StreamCallback = (line: string, source?: string, done?: boolean) => void;

interface SubEntry {
  stream: string;
  params: Record<string, string>;
  cb: StreamCallback;
}

interface LiveStateContextValue {
  payload: LiveStatePayload | null;
  connected: boolean;
  initialized: boolean;
  error: Error | null;
  /** Subscribe to a named stream. Returns a subscription ID. */
  subscribe: (stream: string, params: Record<string, string>, cb: StreamCallback) => string;
  /** Unsubscribe from a stream by ID returned from subscribe(). */
  unsubscribe: (id: string) => void;
}

const LiveStateContext = createContext<LiveStateContextValue | null>(null);

export function LiveStateProvider({ children }: { children: ReactNode }) {
  const [payload, setPayload] = useState<LiveStatePayload | null>(null);
  const [connected, setConnected] = useState(false);
  const [initialized, setInitialized] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const socketRef = useRef<WebSocket | null>(null);
  const retryTimerRef = useRef<number | null>(null);
  const subIdRef = useRef(0);
  const subsRef = useRef<Map<string, SubEntry>>(new Map());

  const sendRaw = useCallback((data: unknown) => {
    if (socketRef.current?.readyState === WebSocket.OPEN) {
      socketRef.current.send(JSON.stringify(data));
    }
  }, []);

  const subscribe = useCallback(
    (stream: string, params: Record<string, string>, cb: StreamCallback): string => {
      const id = `s${++subIdRef.current}`;
      subsRef.current.set(id, { stream, params, cb });
      sendRaw({ type: "subscribe", id, stream, params });
      return id;
    },
    [sendRaw],
  );

  const unsubscribe = useCallback(
    (id: string) => {
      subsRef.current.delete(id);
      sendRaw({ type: "unsubscribe", id });
    },
    [sendRaw],
  );

  useEffect(() => {
    let closed = false;

    const connect = () => {
      const socket = new WebSocket(buildWebSocketUrl("/api/v1/ws/state"));
      socketRef.current = socket;

      socket.onopen = () => {
        setConnected(true);
        setError(null);
        // Re-subscribe all active subscriptions after reconnect.
        for (const [id, entry] of subsRef.current.entries()) {
          socket.send(JSON.stringify({ type: "subscribe", id, stream: entry.stream, params: entry.params }));
        }
      };

      socket.onmessage = (event) => {
        if (typeof event.data !== "string") return;
        try {
          const msg = JSON.parse(event.data) as { type?: string; payload?: LiveStatePayload; id?: string; line?: string; source?: string; done?: boolean };
          if (msg.type === "state" && msg.payload) {
            setPayload(msg.payload);
            setInitialized(true);
          } else if (msg.type === "stream" && msg.id) {
            const entry = subsRef.current.get(msg.id);
            if (entry) {
              entry.cb(msg.line ?? "", msg.source, msg.done === true);
              if (msg.done) {
                subsRef.current.delete(msg.id);
              }
            }
          }
        } catch {
          // Ignore malformed messages.
        }
      };

      socket.onerror = () => {
        setError(new Error("WebSocket connection error"));
      };

      socket.onclose = () => {
        socketRef.current = null;
        setConnected(false);
        if (closed) return;
        retryTimerRef.current = window.setTimeout(connect, 1000);
      };
    };

    connect();

    return () => {
      closed = true;
      if (retryTimerRef.current != null) {
        window.clearTimeout(retryTimerRef.current);
      }
      socketRef.current?.close();
      socketRef.current = null;
    };
  }, []);

  const value = useMemo<LiveStateContextValue>(
    () => ({ payload, connected, initialized, error, subscribe, unsubscribe }),
    [payload, connected, initialized, error, subscribe, unsubscribe],
  );

  return <LiveStateContext.Provider value={value}>{children}</LiveStateContext.Provider>;
}

export function useLiveState() {
  const context = useContext(LiveStateContext);
  if (!context) {
    throw new Error("useLiveState must be used within LiveStateProvider");
  }
  return context;
}
