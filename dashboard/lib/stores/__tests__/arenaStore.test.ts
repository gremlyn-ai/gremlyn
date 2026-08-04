import { describe, it, expect, beforeEach } from "vitest";
import { useArenaStore } from "../arenaStore";
import type { ArenaSession, ArenaEvent } from "@/lib/api/types";

/** Builds a session with every field the Arena API always serializes (models.ArenaSession). */
function makeSession(overrides: Partial<ArenaSession> = {}): ArenaSession {
  return {
    id: "sess-001",
    server_id: "hubspot",
    status: "running",
    config: { gremlins: ["hallucination", "latency"], intensity: "high", prompts: [] },
    gremlins_sent: 0,
    gremlins_survived: 0,
    gremlins_crashed: 0,
    started_at: "2026-04-08T10:00:00Z",
    ...overrides,
  };
}

/** Builds an event with every field the Arena API always serializes (models.ArenaEvent). */
function makeEvent(overrides: Partial<ArenaEvent> = {}): ArenaEvent {
  return {
    id: "evt-001",
    session_id: "sess-001",
    gremlin_type: "hallucination",
    gremlin_config: {},
    injected_at: "2026-04-08T10:01:00Z",
    outcome: "survived",
    score: 100,
    ...overrides,
  };
}

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
    const session = makeSession();

    useArenaStore.getState().setSession(session);
    expect(useArenaStore.getState().currentSession).toEqual(session);
  });

  it("setSession to null clears the session", () => {
    useArenaStore.getState().setSession(makeSession());

    useArenaStore.getState().setSession(null);
    expect(useArenaStore.getState().currentSession).toBeNull();
  });

  it("addEvent appends to events array", () => {
    const event = makeEvent({ score: 85 });

    useArenaStore.getState().addEvent(event);
    expect(useArenaStore.getState().events).toHaveLength(1);
    expect(useArenaStore.getState().events[0]).toEqual(event);
  });

  it("addEvent accumulates multiple events without loss", () => {
    const events: ArenaEvent[] = [
      makeEvent({ id: "evt-001", injected_at: "2026-04-08T10:01:00Z" }),
      makeEvent({
        id: "evt-002",
        gremlin_type: "latency",
        outcome: "degraded",
        score: 50,
        injected_at: "2026-04-08T10:01:01Z",
      }),
      makeEvent({
        id: "evt-003",
        gremlin_type: "corruption",
        outcome: "crashed",
        score: 0,
        injected_at: "2026-04-08T10:01:02Z",
      }),
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
    useArenaStore.getState().addEvent(makeEvent());

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
