import { describe, it, expect, beforeEach } from "vitest";
import { useShieldStore } from "../shieldStore";
import type { Rule, ShieldEvent, ShieldMetricsResponse } from "@/lib/api/types";

describe("useShieldStore", () => {
  beforeEach(() => {
    useShieldStore.setState({
      recentEvents: [],
      rules: [],
      alerts: [],
      metrics: null,
      status: null,
      loading: false,
      error: null,
    });
  });

  it("initializes with empty state", () => {
    const state = useShieldStore.getState();
    expect(state.recentEvents).toEqual([]);
    expect(state.rules).toEqual([]);
    expect(state.alerts).toEqual([]);
    expect(state.metrics).toBeNull();
    expect(state.status).toBeNull();
    expect(state.loading).toBe(false);
    expect(state.error).toBeNull();
  });

  it("setRules updates rules array", () => {
    const rules: Rule[] = [
      {
        id: "rule-001",
        name: "block-bulk-export",
        action: "block",
        enabled: true,
        match: { tool: "search_contacts", args: { limit: { greater_than: 100 } } },
      },
      {
        id: "rule-002",
        name: "block-delete",
        action: "block_and_alert",
        enabled: true,
        match: { tool: "delete_contact" },
      },
    ];

    useShieldStore.getState().setRules(rules);
    expect(useShieldStore.getState().rules).toHaveLength(2);
    expect(useShieldStore.getState().rules[0].name).toBe("block-bulk-export");
  });

  it("toggleRule flips enabled flag for matching rule ID", () => {
    const rules: Rule[] = [
      { id: "rule-001", name: "block-export", action: "block", enabled: true },
      { id: "rule-002", name: "scan-responses", action: "block_and_alert", enabled: true },
    ];

    useShieldStore.getState().setRules(rules);
    useShieldStore.getState().toggleRule("rule-001");

    const updated = useShieldStore.getState().rules;
    expect(updated[0].enabled).toBe(false);
    expect(updated[1].enabled).toBe(true); // Unaffected.
  });

  it("toggleRule does not affect other rules", () => {
    const rules: Rule[] = [
      { id: "r1", name: "rule-a", action: "block", enabled: true },
      { id: "r2", name: "rule-b", action: "redact", enabled: false },
      { id: "r3", name: "rule-c", action: "log_only", enabled: true },
    ];

    useShieldStore.getState().setRules(rules);
    useShieldStore.getState().toggleRule("r2");

    const updated = useShieldStore.getState().rules;
    expect(updated[0].enabled).toBe(true);
    expect(updated[1].enabled).toBe(true); // Toggled from false.
    expect(updated[2].enabled).toBe(true);
  });

  it("toggleRule with non-existent ID is a no-op", () => {
    const rules: Rule[] = [
      { id: "r1", name: "rule-a", action: "block", enabled: true },
    ];

    useShieldStore.getState().setRules(rules);
    useShieldStore.getState().toggleRule("nonexistent");

    expect(useShieldStore.getState().rules[0].enabled).toBe(true);
  });

  it("setMetrics updates metrics", () => {
    const metrics: ShieldMetricsResponse = {
      total_events: 1500,
      blocked_events: 42,
      allowed_events: 1400,
      redacted_events: 58,
      avg_latency_ms: 12,
      events_per_minute: 25,
    };

    useShieldStore.getState().setMetrics(metrics);
    expect(useShieldStore.getState().metrics).toEqual(metrics);
    expect(useShieldStore.getState().metrics?.blocked_events).toBe(42);
  });

  it("setEvents updates recentEvents", () => {
    const events: ShieldEvent[] = [
      {
        id: "evt-001",
        server_id: "hubspot",
        timestamp: "2026-04-08T10:00:00Z",
        direction: "outgoing",
        message_type: "jsonrpc",
        tool_name: "search_contacts",
        action_taken: "blocked",
        rules_triggered: ["block-bulk-export"],
        latency_ms: 3,
      },
      {
        id: "evt-002",
        server_id: "hubspot",
        timestamp: "2026-04-08T10:00:01Z",
        direction: "outgoing",
        message_type: "jsonrpc",
        tool_name: "get_contact",
        action_taken: "allowed",
        latency_ms: 1,
      },
    ];

    useShieldStore.getState().setEvents(events);
    expect(useShieldStore.getState().recentEvents).toHaveLength(2);
    expect(useShieldStore.getState().recentEvents[0].action_taken).toBe("blocked");
  });

  it("setError sets and clears error", () => {
    useShieldStore.getState().setError("Shield offline");
    expect(useShieldStore.getState().error).toBe("Shield offline");

    useShieldStore.getState().setError(null);
    expect(useShieldStore.getState().error).toBeNull();
  });
});
