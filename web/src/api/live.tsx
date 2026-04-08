import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
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

interface LiveStateMessage {
  type: "state";
  payload: LiveStatePayload;
}

interface LiveStateContextValue {
  payload: LiveStatePayload | null;
  connected: boolean;
  initialized: boolean;
  error: Error | null;
}

const LiveStateContext = createContext<LiveStateContextValue | null>(null);

export function LiveStateProvider({ children }: { children: ReactNode }) {
  const [payload, setPayload] = useState<LiveStatePayload | null>(null);
  const [connected, setConnected] = useState(false);
  const [initialized, setInitialized] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const retryTimerRef = useRef<number | null>(null);

  useEffect(() => {
    let closed = false;
    let socket: WebSocket | null = null;

    const connect = () => {
      socket = new WebSocket(buildWebSocketUrl("/api/v1/ws/state"));

      socket.onopen = () => {
        setConnected(true);
        setError(null);
      };

      socket.onmessage = (event) => {
        if (typeof event.data !== "string") {
          return;
        }
        try {
          const message = JSON.parse(event.data) as LiveStateMessage;
          if (message.type === "state" && message.payload) {
            setPayload(message.payload);
            setInitialized(true);
          }
        } catch {
          // Ignore malformed payloads.
        }
      };

      socket.onerror = () => {
        setError(new Error("WebSocket connection error"));
      };

      socket.onclose = () => {
        setConnected(false);
        if (closed) {
          return;
        }
        retryTimerRef.current = window.setTimeout(connect, 1000);
      };
    };

    connect();

    return () => {
      closed = true;
      if (retryTimerRef.current != null) {
        window.clearTimeout(retryTimerRef.current);
      }
      if (socket) {
        socket.close();
      }
    };
  }, []);

  const value = useMemo<LiveStateContextValue>(
    () => ({ payload, connected, initialized, error }),
    [payload, connected, initialized, error],
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
