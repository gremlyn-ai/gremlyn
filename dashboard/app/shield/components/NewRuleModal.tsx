"use client";

import { useState } from "react";
import type { RuleAction } from "@/lib/api/types";

const RULE_ACTIONS: RuleAction[] = [
  "block",
  "block_and_alert",
  "redact",
  "redact_and_alert",
  "allow",
  "throttle",
  "pause_and_request_approval",
  "log_only",
];

type NewRuleForm = {
  name: string;
  action: RuleAction;
  detect: string;
  matchTool: string;
  scanResponses: boolean;
  enabled: boolean;
};

const INITIAL_FORM: NewRuleForm = {
  name: "",
  action: "block",
  detect: "",
  matchTool: "",
  scanResponses: false,
  enabled: true,
};

type NewRuleModalProps = {
  onSubmit: (rule: {
    name: string;
    action: RuleAction;
    detect?: string[];
    match_tool?: string;
    scan_responses?: boolean;
    enabled: boolean;
  }) => Promise<void>;
  onClose: () => void;
};

export function NewRuleModal({ onSubmit, onClose }: NewRuleModalProps) {
  const [form, setForm] = useState<NewRuleForm>(INITIAL_FORM);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const canSubmit = form.name.trim().length > 0 && (form.detect.trim().length > 0 || form.matchTool.trim().length > 0);

  async function handleSubmit() {
    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);
    try {
      const detectValues = form.detect
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);

      await onSubmit({
        name: form.name.trim(),
        action: form.action,
        detect: detectValues.length > 0 ? detectValues : undefined,
        match_tool: form.matchTool.trim() || undefined,
        scan_responses: form.scanResponses || undefined,
        enabled: form.enabled,
      });
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create rule");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60" onClick={onClose} />
      <div className="relative bg-surface-container-low border border-outline-variant/20 w-full max-w-lg mx-4">
        {/* Header */}
        <div className="flex items-center justify-between p-6 border-b border-outline-variant/10">
          <h4 className="font-headline text-lg font-bold uppercase tracking-tight text-primary">
            NEW_POLICY
          </h4>
          <button onClick={onClose} className="text-on-surface-variant hover:text-error transition-colors">
            <span className="material-symbols-outlined">close</span>
          </button>
        </div>

        {/* Form */}
        <div className="p-6 space-y-4">
          {error && (
            <div className="bg-error/10 border border-error/20 p-3">
              <span className="font-mono text-xs text-error">[ERROR] {error}</span>
            </div>
          )}

          {/* Name */}
          <div>
            <label className="font-mono text-[10px] text-on-surface-variant uppercase tracking-widest block mb-1">
              POLICY_NAME
            </label>
            <input
              type="text"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="e.g. block_sql_injection"
              className="w-full bg-surface-container-lowest border-0 text-xs font-mono py-2.5 px-4 focus:ring-1 focus:ring-primary text-on-surface placeholder:text-on-surface-variant/30"
            />
          </div>

          {/* Action */}
          <div>
            <label className="font-mono text-[10px] text-on-surface-variant uppercase tracking-widest block mb-1">
              ACTION
            </label>
            <select
              value={form.action}
              onChange={(e) => setForm({ ...form, action: e.target.value as RuleAction })}
              className="w-full bg-surface-container-lowest border-0 text-xs font-mono py-2.5 px-4 focus:ring-1 focus:ring-primary text-on-surface uppercase"
            >
              {RULE_ACTIONS.map((a) => (
                <option key={a} value={a}>
                  {a.toUpperCase().replace(/_/g, " ")}
                </option>
              ))}
            </select>
          </div>

          {/* Detect */}
          <div>
            <label className="font-mono text-[10px] text-on-surface-variant uppercase tracking-widest block mb-1">
              DETECT (comma-separated keywords)
            </label>
            <input
              type="text"
              value={form.detect}
              onChange={(e) => setForm({ ...form, detect: e.target.value })}
              placeholder="e.g. sql_injection, prompt_injection"
              className="w-full bg-surface-container-lowest border-0 text-xs font-mono py-2.5 px-4 focus:ring-1 focus:ring-primary text-on-surface placeholder:text-on-surface-variant/30"
            />
          </div>

          {/* Match tool */}
          <div>
            <label className="font-mono text-[10px] text-on-surface-variant uppercase tracking-widest block mb-1">
              MATCH_TOOL (optional)
            </label>
            <input
              type="text"
              value={form.matchTool}
              onChange={(e) => setForm({ ...form, matchTool: e.target.value })}
              placeholder="e.g. file_read, execute_command"
              className="w-full bg-surface-container-lowest border-0 text-xs font-mono py-2.5 px-4 focus:ring-1 focus:ring-primary text-on-surface placeholder:text-on-surface-variant/30"
            />
          </div>

          {/* Toggles row */}
          <div className="flex gap-6">
            <label className="flex items-center gap-3 cursor-pointer">
              <button
                type="button"
                onClick={() => setForm({ ...form, scanResponses: !form.scanResponses })}
                className={`relative inline-flex h-5 w-10 shrink-0 items-center transition-colors ${
                  form.scanResponses ? "bg-primary" : "bg-surface-container-highest"
                }`}
              >
                <span className={`inline-block h-3 w-3 transition-transform ${
                  form.scanResponses ? "translate-x-5 bg-on-primary" : "translate-x-1 bg-on-surface-variant"
                }`} />
              </button>
              <span className="font-mono text-[10px] text-on-surface-variant uppercase">SCAN_RESPONSES</span>
            </label>

            <label className="flex items-center gap-3 cursor-pointer">
              <button
                type="button"
                onClick={() => setForm({ ...form, enabled: !form.enabled })}
                className={`relative inline-flex h-5 w-10 shrink-0 items-center transition-colors ${
                  form.enabled ? "bg-primary" : "bg-surface-container-highest"
                }`}
              >
                <span className={`inline-block h-3 w-3 transition-transform ${
                  form.enabled ? "translate-x-5 bg-on-primary" : "translate-x-1 bg-on-surface-variant"
                }`} />
              </button>
              <span className="font-mono text-[10px] text-on-surface-variant uppercase">ENABLED</span>
            </label>
          </div>
        </div>

        {/* Actions */}
        <div className="flex gap-3 p-6 pt-0">
          <button
            onClick={handleSubmit}
            disabled={!canSubmit || submitting}
            className="flex-1 py-3 bg-primary text-on-primary font-mono text-xs font-bold tracking-widest uppercase hover:shadow-[0_0_10px_rgba(142,255,113,0.3)] transition-all disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {submitting ? "CREATING..." : "CREATE_POLICY"}
          </button>
          <button
            onClick={onClose}
            className="px-6 py-3 bg-surface-container-highest text-on-surface-variant font-mono text-xs border border-outline-variant/20 hover:bg-surface-container-high hover:text-on-surface transition-all"
          >
            CANCEL
          </button>
        </div>
      </div>
    </div>
  );
}
