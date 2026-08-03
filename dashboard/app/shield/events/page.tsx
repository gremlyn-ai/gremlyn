"use client";

import { useState } from "react";
import { Sidebar } from "@/components/Sidebar";
import { TopBar } from "@/components/TopBar";
import { useShieldEvents, useShieldStatus } from "@/lib/hooks/useShieldData";
import type { ShieldEvent, ActionTaken } from "@/lib/api/types";

const ACTION_OPTIONS: (ActionTaken | "all")[] = ["all", "allowed", "blocked", "redacted", "alerted"];

function actionBadge(action: ActionTaken) {
  switch (action) {
    case "blocked":
      return "bg-error/10 text-error border-error/20";
    case "redacted":
      return "bg-secondary/10 text-secondary border-secondary/20";
    case "alerted":
      return "bg-tertiary/10 text-tertiary border-tertiary/20";
    default:
      return "bg-primary/10 text-primary border-primary/20";
  }
}

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function formatJSON(data: unknown): string {
  if (!data) return "\u2014";
  try {
    return JSON.stringify(data, null, 2);
  } catch {
    return String(data);
  }
}

export default function EventsPage() {
  const { data: events } = useShieldEvents();
  const { data: status } = useShieldStatus();

  const [actionFilter, setActionFilter] = useState<ActionTaken | "all">("all");
  const [serverFilter, setServerFilter] = useState<string>("all");
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const servers = status?.servers ?? [];

  const filtered = (events ?? []).filter((e) => {
    if (actionFilter !== "all" && e.action_taken !== actionFilter) return false;
    if (serverFilter !== "all" && e.server_id !== serverFilter) return false;
    return true;
  });

  return (
    <div>
      <Sidebar activePage="shield" />

      <main className="flex-1 ml-64 bg-background relative min-h-screen">
        <TopBar accent="primary" statusText="Shield_Events" statusLabel="Shield_Events" />

        <div className="p-8 max-w-7xl mx-auto space-y-8 pb-24">
          {/* Header */}
          <div className="border-b border-outline-variant/20 pb-4">
            <h3 className="text-4xl font-headline font-bold tracking-tight text-on-surface">
              EVENT_LOG
            </h3>
            <p className="text-on-surface-variant font-mono text-sm mt-1 uppercase tracking-tighter">
              {filtered.length} events {actionFilter !== "all" || serverFilter !== "all" ? "(filtered)" : ""}
            </p>
          </div>

          {/* Filters */}
          <div className="flex gap-4 items-center">
            <div>
              <label className="font-mono text-[10px] text-on-surface-variant uppercase tracking-widest block mb-1">
                ACTION
              </label>
              <select
                value={actionFilter}
                onChange={(e) => setActionFilter(e.target.value as ActionTaken | "all")}
                className="bg-surface-container-lowest border-0 text-xs font-mono py-2 px-4 focus:ring-1 focus:ring-primary text-on-surface uppercase"
              >
                {ACTION_OPTIONS.map((a) => (
                  <option key={a} value={a}>{a.toUpperCase()}</option>
                ))}
              </select>
            </div>
            <div>
              <label className="font-mono text-[10px] text-on-surface-variant uppercase tracking-widest block mb-1">
                SERVER
              </label>
              <select
                value={serverFilter}
                onChange={(e) => setServerFilter(e.target.value)}
                className="bg-surface-container-lowest border-0 text-xs font-mono py-2 px-4 focus:ring-1 focus:ring-primary text-on-surface uppercase"
              >
                <option value="all">ALL</option>
                {servers.map((s) => (
                  <option key={s} value={s}>{s.toUpperCase()}</option>
                ))}
              </select>
            </div>
            <div className="ml-auto font-mono text-[10px] text-on-surface-variant">
              TOTAL: <span className="text-on-surface font-bold">{events?.length ?? 0}</span> | SHOWING: <span className="text-primary font-bold">{filtered.length}</span>
            </div>
          </div>

          {/* Events table */}
          <div className="bg-surface-container-low overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full text-left font-mono text-xs">
                <thead>
                  <tr className="text-on-surface-variant border-b border-outline-variant/10 uppercase tracking-widest">
                    <th className="p-4 font-medium">Timestamp</th>
                    <th className="p-4 font-medium">Server</th>
                    <th className="p-4 font-medium">Type</th>
                    <th className="p-4 font-medium">Tool</th>
                    <th className="p-4 font-medium">Action</th>
                    <th className="p-4 font-medium text-right">Latency</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-outline-variant/5">
                  {filtered.length === 0 && (
                    <tr>
                      <td colSpan={6} className="p-8 text-center text-on-surface-variant">
                        NO_EVENTS_MATCH_FILTERS
                      </td>
                    </tr>
                  )}
                  {filtered.map((e: ShieldEvent) => (
                    <tr key={e.id} className="group">
                      <td colSpan={6} className="p-0">
                        <button
                          onClick={() => setExpandedId(expandedId === e.id ? null : e.id)}
                          className="w-full text-left hover:bg-surface-container-high transition-colors"
                        >
                          <div className="grid grid-cols-6 p-4 items-center">
                            <span className="text-on-surface-variant">{formatTime(e.timestamp)}</span>
                            <span className="text-on-surface uppercase">{e.server_id}</span>
                            <span className="text-on-surface-variant">{e.message_type}</span>
                            <span className="text-on-surface">{e.tool_name ?? "—"}</span>
                            <span>
                              <span className={`px-2 py-0.5 text-[10px] border uppercase ${actionBadge(e.action_taken)}`}>
                                {e.action_taken}
                              </span>
                            </span>
                            <span className="text-on-surface-variant text-right">{e.latency_ms}ms</span>
                          </div>
                        </button>

                        {expandedId === e.id && (
                          <div className="px-4 pb-4 grid grid-cols-3 gap-4 font-mono text-[10px] bg-surface-container-lowest mx-4 mb-4 p-4">
                            <div>
                              <span className="text-on-surface-variant block mb-1">TOOL_ARGS</span>
                              <pre className="text-on-surface bg-surface-container p-2 overflow-x-auto max-h-32 overflow-y-auto whitespace-pre-wrap break-all">
                                {formatJSON(e.tool_args)}
                              </pre>
                            </div>
                            <div>
                              <span className="text-on-surface-variant block mb-1">DETECTION_RESULTS</span>
                              <pre className="text-on-surface bg-surface-container p-2 overflow-x-auto max-h-32 overflow-y-auto whitespace-pre-wrap break-all">
                                {formatJSON(e.detection_results)}
                              </pre>
                            </div>
                            <div>
                              <span className="text-on-surface-variant block mb-1">RULES_TRIGGERED</span>
                              <pre className="text-on-surface bg-surface-container p-2 overflow-x-auto max-h-32 overflow-y-auto whitespace-pre-wrap break-all">
                                {e.rules_triggered && e.rules_triggered.length > 0
                                  ? e.rules_triggered.join("\n")
                                  : "\u2014"}
                              </pre>
                            </div>
                          </div>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}
