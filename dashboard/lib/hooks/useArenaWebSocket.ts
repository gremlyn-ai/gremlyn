import { useRef, useCallback } from "react";
import { ArenaWebSocket } from "@/lib/websocket";
import { useArenaStore } from "@/lib/stores/arenaStore";
import type { ArenaEvent } from "@/lib/api/types";

export function useArenaWebSocket(sessionId: string | null) {
  const wsRef = useRef<ArenaWebSocket | null>(null);
  const addEvent = useArenaStore((s) => s.addEvent);
  const setWsConnected = useArenaStore((s) => s.setWsConnected);

  const connect = useCallback(() => {
    if (!sessionId) return;
    wsRef.current?.disconnect();

    const ws = new ArenaWebSocket(sessionId);
    ws.subscribe((event: ArenaEvent) => addEvent(event));
    ws.connect();
    wsRef.current = ws;

    const interval = setInterval(() => {
      setWsConnected(ws.connected);
    }, 1000);

    return () => {
      clearInterval(interval);
      ws.disconnect();
      setWsConnected(false);
    };
  }, [sessionId, addEvent, setWsConnected]);

  const disconnect = useCallback(() => {
    wsRef.current?.disconnect();
    wsRef.current = null;
    setWsConnected(false);
  }, [setWsConnected]);

  return { connect, disconnect, wsRef };
}
