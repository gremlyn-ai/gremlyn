import { describe, it, expect, beforeEach } from "vitest";
import { useArenaStore } from "../arenaStore";
import type { ArenaSession, ArenaEvent } from "@/lib/api/types";

describe("useArenaStore", () => {
  beforeEach(() => {
    useArenaStore.setState({
      currentSession: null,
      events: [],
      gremlins: [],
      wsConnected: false,
      loading: false,
      error: null,
      showTerminal: false,
    });
  });

  it("initializes with null session and empty events", () => {
    const state = useArenaStore.getState();
    expect(state.currentSession).toBeNull();
    expect(state.events).toEqual([]);
    expect(state.gremlins).toEqual([]);
    expect(state.wsConnected).toBe(false);
    expect(state.loading).toBe(false);
    expect(state.error).toBeNull();
    expect(state.showTerminal).toBe(false);
  });

  it("setSession updates currentSession", () => {
    const session: ArenaSession = {
      id: "sess-001",
      status: "running",
      config: { gremlins: ["hallucination", "latency"], intensity: 0.7 },
      created_at: "2026-04-08T10:00:00Z",
    };

    useArenaStore.getState().setSession(session);
    expect(useArenaStore.getState().currentSession).toEqual(session);
  });

  it("setSession to null clears the session", () => {
    useArenaStore.getState().setSession({
      id: "sess-001",
      status: "running",
      config: { gremlins: ["hallucination"], intensity: 0.5 },
      created_at: "2026-04-08T10:00:00Z",
    });

    useArenaStore.getState().setSession(null);
    expect(useArenaStore.getState().currentSession).toBeNull();
  });

  it("addEvent appends to events array", () => {
    const event: ArenaEvent = {
      id: "evt-001",
      session_id: "sess-001",
      gremlin_type: "hallucination",
      outcome: "survived",
      score: 85,
      timestamp: "2026-04-08T10:01:00Z",
    };

    useArenaStore.getState().addEvent(event);
    expect(useArenaStore.getState().events).toHaveLength(1);
    expect(useArenaStore.getState().events[0]).toEqual(event);
  });

  it("addEvent accumulates multiple events without loss", () => {
    const events: ArenaEvent[] = [
      { id: "evt-001", session_id: "s1", gremlin_type: "hallucination", outcome: "survived", score: 100, timestamp: "2026-04-08T10:01:00Z" },
      { id: "evt-002", session_id: "s1", gremlin_type: "latency", outcome: "degraded", score: 50, timestamp: "2026-04-08T10:01:01Z" },
      { id: "evt-003", session_id: "s1", gremlin_type: "corruption", outcome: "crashed", score: 0, timestamp: "2026-04-08T10:01:02Z" },
    ];

    for (const e of events) {
      useArenaStore.getState().addEvent(e);
    }

    expect(useArenaStore.getState().events).toHaveLength(3);
    expect(useArenaStore.getState().events.map((e) => e.id)).toEqual([
      "evt-001",
      "evt-002",
      "evt-003",
    ]);
  });

  it("clearEvents resets to empty", () => {
    useArenaStore.getState().addEvent({
      id: "evt-001",
      session_id: "s1",
      gremlin_type: "hallucination",
      outcome: "survived",
      score: 100,
      timestamp: "2026-04-08T10:01:00Z",
    });

    useArenaStore.getState().clearEvents();
    expect(useArenaStore.getState().events).toEqual([]);
  });

  it("toggleTerminal flips showTerminal", () => {
    expect(useArenaStore.getState().showTerminal).toBe(false);

    useArenaStore.getState().toggleTerminal();
    expect(useArenaStore.getState().showTerminal).toBe(true);

    useArenaStore.getState().toggleTerminal();
    expect(useArenaStore.getState().showTerminal).toBe(false);
  });

  it("setError sets and clears error message", () => {
    useArenaStore.getState().setError("Connection failed");
    expect(useArenaStore.getState().error).toBe("Connection failed");

    useArenaStore.getState().setError(null);
    expect(useArenaStore.getState().error).toBeNull();
  });

  it("setLoading toggles loading state", () => {
    useArenaStore.getState().setLoading(true);
    expect(useArenaStore.getState().loading).toBe(true);

    useArenaStore.getState().setLoading(false);
    expect(useArenaStore.getState().loading).toBe(false);
  });

  it("setWsConnected tracks WebSocket state", () => {
    useArenaStore.getState().setWsConnected(true);
    expect(useArenaStore.getState().wsConnected).toBe(true);

    useArenaStore.getState().setWsConnected(false);
    expect(useArenaStore.getState().wsConnected).toBe(false);
  });
});
