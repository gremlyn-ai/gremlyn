"use client";

import { useState } from "react";
import { Sidebar } from "@/components/Sidebar";
import { TopBar } from "@/components/TopBar";
import { RuleTable } from "../components/RuleTable";
import { NewRuleModal } from "../components/NewRuleModal";
import { useShieldRules } from "@/lib/hooks/useShieldData";
import { useShieldStore } from "@/lib/stores/shieldStore";
import { toggleRule, createRule } from "@/lib/api/shield";

export default function RulesPage() {
  const { data: rules, mutate: mutateRules } = useShieldRules();
  const [showNewRule, setShowNewRule] = useState(false);

  async function handleToggleRule(ruleId: string, enabled: boolean) {
    useShieldStore.getState().toggleRule(ruleId);
    try {
      await toggleRule(ruleId, enabled);
      mutateRules();
    } catch {
      useShieldStore.getState().toggleRule(ruleId);
    }
  }

  async function handleCreateRule(rule: Parameters<typeof createRule>[0]) {
    await createRule(rule);
    mutateRules();
  }

  const activeCount = rules?.filter((r) => r.enabled).length ?? 0;
  const totalCount = rules?.length ?? 0;

  return (
    <div>
      <Sidebar activePage="shield" />

      <main className="flex-1 ml-64 bg-background relative min-h-screen">
        <TopBar accent="primary" statusText="Shield_Rules" statusLabel="Shield_Rules" />

        <div className="p-8 max-w-7xl mx-auto space-y-8 pb-24">
          {/* Header */}
          <div className="flex items-end justify-between border-b border-outline-variant/20 pb-4">
            <div>
              <h3 className="text-4xl font-headline font-bold tracking-tight text-on-surface">
                ENFORCEMENT_RULES
              </h3>
              <p className="text-on-surface-variant font-mono text-sm mt-1 uppercase tracking-tighter">
                {activeCount} active / {totalCount} total policies
              </p>
            </div>
            <button
              onClick={() => setShowNewRule(true)}
              className="bg-primary text-on-primary px-4 py-2 font-mono text-xs font-bold hover:shadow-[0_0_10px_rgba(142,255,113,0.3)] transition-all"
            >
              NEW_POLICY
            </button>
          </div>

          {/* Rules table */}
          <RuleTable rules={rules ?? []} onToggleRule={handleToggleRule} />
        </div>
      </main>

      {showNewRule && (
        <NewRuleModal
          onSubmit={handleCreateRule}
          onClose={() => setShowNewRule(false)}
        />
      )}
    </div>
  );
}
