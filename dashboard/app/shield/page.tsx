"use client";

import { useState } from "react";
import { Sidebar } from "@/components/Sidebar";
import { TopBar } from "@/components/TopBar";
import { StatusFooter } from "@/components/StatusFooter";
import { MetricCards } from "./components/MetricCards";
import { ThreatCard } from "./components/ThreatCard";
import { ThreatChart } from "./components/ThreatChart";
import { RuleTable } from "./components/RuleTable";
import { useShieldStatus, useShieldMetrics, useBlockedEvents, useShieldRules, useShieldEvents } from "@/lib/hooks/useShieldData";
import { NewRuleModal } from "./components/NewRuleModal";
import { useShieldStore } from "@/lib/stores/shieldStore";
import { toggleRule, createRule } from "@/lib/api/shield";

export default function ShieldDashboard() {
  const { data: status, error: statusError } = useShieldStatus();
  const { data: metrics } = useShieldMetrics();
  const { data: blockedEvents } = useBlockedEvents(1);
  const { data: rules, mutate: mutateRules } = useShieldRules();
  const { data: events } = useShieldEvents();
  const [showNewRule, setShowNewRule] = useState(false);

  const latestBlocked = blockedEvents?.[0] ?? null;

  async function handleCreateRule(rule: Parameters<typeof createRule>[0]) {
    await createRule(rule);
    mutateRules();
  }

  function handleExportLogs() {
    if (!events || events.length === 0) return;
    const json = JSON.stringify(events, null, 2);
    const blob = new Blob([json], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `gremlyn-shield-events-${Date.now()}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }

  async function handleToggleRule(ruleId: string, enabled: boolean) {
    useShieldStore.getState().toggleRule(ruleId);
    try {
      await toggleRule(ruleId, enabled);
      mutateRules();
    } catch {
      useShieldStore.getState().toggleRule(ruleId);
    }
  }

  return (
    <>
      {/* Scanline overlay */}
      <div className="fixed inset-0 scanline-shield z-50 pointer-events-none" />

      <Sidebar activePage="shield" />

      <main className="ml-64 min-h-screen bg-surface flex flex-col">
        <TopBar accent="primary" statusText="Shield_Active" statusLabel="Shield_Active" nodeCount={status?.servers?.length} isOnline={!statusError} />

        <section className="p-8 space-y-8">
          {/* Error banner */}
          {statusError && (
            <div className="bg-error/10 border border-error/20 p-4 flex items-center justify-between">
              <span className="font-mono text-xs text-error">[ERROR] Shield API unreachable</span>
            </div>
          )}

          {/* Page header */}
          <div className="flex justify-between items-end border-b border-outline-variant/10 pb-6">
            <div>
              <h3 className="font-headline text-4xl font-bold tracking-tight text-on-surface">
                SHIELD_DASHBOARD
              </h3>
              <p className="text-on-surface-variant font-mono text-sm mt-1 uppercase tracking-tighter">
                Real-time threat mitigation and agent orchestration
              </p>
            </div>
            <div className="flex gap-2">
              <button
                onClick={handleExportLogs}
                disabled={!events || events.length === 0}
                className="bg-surface-container-highest text-primary px-4 py-2 font-mono text-xs border border-primary/20 hover:bg-primary hover:text-on-primary transition-all disabled:opacity-40 disabled:cursor-not-allowed"
              >
                EXPORT_LOGS
              </button>
              <button
                onClick={() => setShowNewRule(true)}
                className="bg-primary text-on-primary px-4 py-2 font-mono text-xs font-bold hover:shadow-[0_0_10px_rgba(142,255,113,0.3)] transition-all"
              >
                NEW_POLICY
              </button>
            </div>
          </div>

          {/* Metrics row */}
          <MetricCards
            blockedCount={metrics?.event_counts?.blocked ?? null}
            serverCount={status?.servers?.length ?? null}
            uptime={status?.uptime ?? null}
            alertCounts={metrics?.alert_counts ?? null}
          />

          {/* Threat + Chart row */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
            <ThreatCard event={latestBlocked} />
            <ThreatChart events={events ?? []} />
          </div>

          {/* Rules table */}
          <RuleTable
            rules={rules ?? []}
            onToggleRule={handleToggleRule}
          />
        </section>

        <StatusFooter shieldOnline={!statusError} />
      </main>

      {showNewRule && (
        <NewRuleModal
          onSubmit={handleCreateRule}
          onClose={() => setShowNewRule(false)}
        />
      )}
    </>
  );
}
