import type { WSMessage, ArenaEvent } from "./api/types";

type EventHandler = (event: ArenaEvent) => void;

export type ConnectionState = "disconnected" | "connecting" | "connected" | "reconnecting";

const ARENA_WS_BASE = process.env.NEXT_PUBLIC_ARENA_WS_URL ?? "ws://localhost:8082";

const MAX_RETRIES = 10;
const INITIAL_BACKOFF_MS = 1000;
const MAX_BACKOFF_MS = 30000;

/**
 * WebSocket manager for live Arena session streaming.
 * Handles connection, exponential backoff reconnect, and typed message dispatch.
 */
export class ArenaWebSocket {
  private ws: WebSocket | null = null;
  private sessionId: string;
  private handlers: Set<EventHandler> = new Set();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private shouldReconnect = true;
  private retryCount = 0;
  private _state: ConnectionState = "disconnected";
  private stateListeners: Set<(state: ConnectionState) => void> = new Set();

  constructor(sessionId: string) {
    this.sessionId = sessionId;
  }

  connect(): void {
    if (this.ws?.readyState === WebSocket.OPEN) return;

    this.setState(this.retryCount > 0 ? "reconnecting" : "connecting");

    const url = `${ARENA_WS_BASE}/arena/sessions/${this.sessionId}/ws`;
    this.ws = new WebSocket(url);

    this.ws.onopen = () => {
      this.clearReconnectTimer();
      this.retryCount = 0;
      this.setState("connected");
    };

    this.ws.onmessage = (event: MessageEvent) => {
      try {
        const msg = JSON.parse(event.data as string) as WSMessage;
        if (msg.type === "arena_event") {
          const arenaEvent = msg.data as ArenaEvent;
          this.handlers.forEach((handler) => handler(arenaEvent));
        }
      } catch {
        // Ignore malformed messages.
      }
    };

    this.ws.onclose = () => {
      if (this.shouldReconnect && this.retryCount < MAX_RETRIES) {
        this.setState("reconnecting");
        this.scheduleReconnect();
      } else {
        this.setState("disconnected");
      }
    };

    this.ws.onerror = () => {
      this.ws?.close();
    };
  }

  disconnect(): void {
    this.shouldReconnect = false;
    this.clearReconnectTimer();
    this.ws?.close();
    this.ws = null;
    this.retryCount = 0;
    this.setState("disconnected");
  }

  subscribe(handler: EventHandler): () => void {
    this.handlers.add(handler);
    return () => {
      this.handlers.delete(handler);
    };
  }

  /** Subscribe to connection state changes. */
  onStateChange(listener: (state: ConnectionState) => void): () => void {
    this.stateListeners.add(listener);
    return () => {
      this.stateListeners.delete(listener);
    };
  }

  get state(): ConnectionState {
    return this._state;
  }

  get connected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  private setState(state: ConnectionState): void {
    this._state = state;
    this.stateListeners.forEach((listener) => listener(state));
  }

  private scheduleReconnect(): void {
    this.clearReconnectTimer();
    const backoff = Math.min(
      INITIAL_BACKOFF_MS * Math.pow(2, this.retryCount),
      MAX_BACKOFF_MS,
    );
    this.retryCount++;
    this.reconnectTimer = setTimeout(() => this.connect(), backoff);
  }

  private clearReconnectTimer(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }
}
