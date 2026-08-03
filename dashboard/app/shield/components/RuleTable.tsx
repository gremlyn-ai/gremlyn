"use client";

import { useState } from "react";
import type { Rule, RuleAction } from "@/lib/api/types";

type RuleTableProps = {
  rules: Rule[];
  onToggleRule: (ruleId: string, enabled: boolean) => void;
};

const BLOCK_ACTIONS: RuleAction[] = ["block", "block_and_alert"];

function formatDetect(rule: Rule): string {
  if (rule.detect) {
    return Array.isArray(rule.detect) ? rule.detect.join(", ") : rule.detect;
  }
  if (rule.match?.tool) {
    return `tool: ${rule.match.tool}`;
  }
  return rule.scan_responses ? "scan_responses" : "—";
}

function formatAction(action: RuleAction): string {
  return action.toUpperCase().replace(/_/g, "_");
}

export function RuleTable({ rules, onToggleRule }: RuleTableProps) {
  const [filter, setFilter] = useState("");

  const filtered = filter
    ? rules.filter((r) => {
        const q = filter.toLowerCase();
        return r.name.toLowerCase().includes(q) || formatDetect(r).toLowerCase().includes(q);
      })
    : rules;

  return (
    <div className="bg-surface-container-low overflow-hidden">
      {/* Header */}
      <div className="p-6 border-b border-outline-variant/10 flex justify-between items-center">
        <h4 className="font-headline text-xl font-bold uppercase tracking-tight">
          ACTIVE_ENFORCEMENT_RULES
        </h4>
        <div className="flex items-center gap-4">
          <div className="relative">
            <span className="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-on-surface-variant text-sm">
              search
            </span>
            <input
              className="bg-surface-container-lowest border-0 text-[10px] font-mono py-2 pl-9 pr-4 w-48 focus:ring-1 focus:ring-primary text-on-surface"
              placeholder="FILTER RULES..."
              type="text"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
          </div>
          <span className="text-xs text-on-surface-variant font-mono">
            {rules.filter((r) => r.enabled).length} ACTIVE
          </span>
        </div>
      </div>

      {/* Table */}
      <div className="overflow-x-auto">
        <table className="w-full text-left font-mono text-xs">
          <thead>
            <tr className="text-on-surface-variant border-b border-outline-variant/10 uppercase tracking-widest">
              <th className="p-6 font-medium">Policy Name</th>
              <th className="p-6 font-medium">Trigger</th>
              <th className="p-6 font-medium">Action</th>
              <th className="p-6 font-medium">Scope</th>
              <th className="p-6 font-medium text-right">Enforcement</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-outline-variant/5">
            {filtered.length === 0 && (
              <tr>
                <td colSpan={5} className="p-6 text-center text-on-surface-variant">
                  {filter ? "NO_MATCHING_RULES" : "No rules loaded"}
                </td>
              </tr>
            )}
            {filtered.map((rule) => {
              const isBlockAction = BLOCK_ACTIONS.includes(rule.action);
              return (
                <tr
                  key={rule.id}
                  className="hover:bg-surface-container-high transition-colors group"
                >
                  <td className="p-6 font-bold text-on-surface">{rule.name}</td>
                  <td className="p-6 text-on-surface-variant">{formatDetect(rule)}</td>
                  <td className="p-6">
                    {isBlockAction ? (
                      <span className="bg-error/10 text-error px-2 py-0.5 border border-error/20">
                        {formatAction(rule.action)}
                      </span>
                    ) : (
                      <span className="bg-primary/10 text-primary px-2 py-0.5 border border-primary/20">
                        {formatAction(rule.action)}
                      </span>
                    )}
                  </td>
                  <td className="p-6 text-on-surface-variant">
                    {rule.scan_responses ? "IN+OUT" : rule.scan_outgoing ? "OUT" : "IN"}
                  </td>
                  <td className="p-6 text-right">
                    <button
                      onClick={() => onToggleRule(rule.id, !rule.enabled)}
                      className={`relative inline-flex h-5 w-10 shrink-0 cursor-pointer items-center transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary ${
                        rule.enabled
                          ? "bg-primary"
                          : "bg-surface-container-highest"
                      }`}
                    >
                      <span
                        className={`inline-block h-3 w-3 transition-transform ${
                          rule.enabled
                            ? "translate-x-5 bg-on-primary"
                            : "translate-x-1 bg-on-surface-variant"
                        }`}
                      />
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
