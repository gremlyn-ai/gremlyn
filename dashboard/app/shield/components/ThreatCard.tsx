"use client";

import { useState } from "react";
import type { ShieldEvent } from "@/lib/api/types";

type ThreatCardProps = {
  event: ShieldEvent | null;
};

function truncateId(id: string): string {
  return id.length > 16 ? id.slice(-16) : id;
}

function formatJSON(data: unknown): string {
  if (!data) return "\u2014";
  try {
    return JSON.stringify(data, null, 2);
  } catch {
    return String(data);
  }
}

function downloadEventReport(event: ShieldEvent) {
  const json = JSON.stringify(event, null, 2);
  const blob = new Blob([json], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `gremlyn-threat-report-${event.id.slice(0, 8)}.json`;
  a.click();
  URL.revokeObjectURL(url);
}

function formatTimestamp(ts: string): string {
  try {
    const d = new Date(ts);
    return d.toLocaleTimeString("en-US", { hour12: false, fractionalSecondDigits: 3 });
  } catch {
    return ts;
  }
}

export function ThreatCard({ event }: ThreatCardProps) {
  const [showReplay, setShowReplay] = useState(false);

  if (!event) {
    return (
      <div className="lg:col-span-2 bg-surface-container-low relative overflow-hidden flex flex-col h-full items-center justify-center p-12">
        <span className="material-symbols-outlined text-primary text-5xl mb-4">verified_user</span>
        <h4 className="font-headline text-2xl font-bold mb-2 uppercase text-primary">
          NO_RECENT_THREATS
        </h4>
        <p className="font-mono text-xs text-on-surface-variant">All systems nominal</p>
      </div>
    );
  }

  const isCritical = event.action_taken === "blocked";
  const detectionVector = event.detection_results
    ? Object.keys(event.detection_results).join(", ")
    : event.message_type;

  return (
    <div className="lg:col-span-2 bg-surface-container-low relative overflow-hidden flex flex-col h-full group">
      {isCritical && (
        <div className="absolute top-0 right-0 p-4">
          <span className="bg-error/10 text-error px-3 py-1 text-[10px] font-mono font-bold border border-error/30 uppercase tracking-widest">
            CRITICAL_EVENT
          </span>
        </div>
      )}

      <div className="p-8">
        <h4 className="font-headline text-2xl font-bold mb-1 uppercase">
          LATEST_BLOCKED_THREAT
        </h4>
        <p className="font-mono text-xs text-on-surface-variant mb-6">
          UUID: {truncateId(event.id)}
        </p>

        <div className="grid grid-cols-2 md:grid-cols-3 gap-8 mb-8">
          <div>
            <p className="text-[10px] font-mono text-on-surface-variant mb-1 uppercase tracking-widest">
              Tool Targeted
            </p>
            <p className="font-body font-bold text-on-surface">
              {event.tool_name ?? "UNKNOWN"}
            </p>
          </div>
          <div>
            <p className="text-[10px] font-mono text-on-surface-variant mb-1 uppercase tracking-widest">
              Server
            </p>
            <p className="font-mono font-bold text-primary">
              {event.server_id}
            </p>
          </div>
          <div>
            <p className="text-[10px] font-mono text-on-surface-variant mb-1 uppercase tracking-widest">
              Detection Vector
            </p>
            <p className="font-body font-bold text-on-surface">
              {detectionVector}
            </p>
          </div>
        </div>

        <div className="bg-surface-container-lowest p-6 border-l border-error/40 font-mono text-[11px] text-on-surface-variant leading-relaxed">
          <span className="text-error">[DETECTED]</span> {formatTimestamp(event.timestamp)} - {event.message_type} on {event.tool_name ?? "unknown"}
          <br />
          {event.rules_triggered && event.rules_triggered.length > 0 && (
            <>
              <span className="text-error">[RULES]</span> {event.rules_triggered.join(", ")}
              <br />
            </>
          )}
          <span className="text-primary">[SHIELDED]</span> Action: {event.action_taken.toUpperCase()} — latency {event.latency_ms}ms
        </div>
      </div>

      {showReplay && (
        <div className="px-8 pb-4">
          <div className="bg-surface-container-lowest border border-outline-variant/20 p-4 space-y-3 font-mono text-[10px]">
            <div className="flex items-center justify-between mb-2">
              <span className="text-primary font-bold uppercase tracking-widest text-xs">ATTACK_REPLAY</span>
              <button onClick={() => setShowReplay(false)} className="text-on-surface-variant hover:text-error transition-colors">
                <span className="material-symbols-outlined text-sm">close</span>
              </button>
            </div>
            <div>
              <span className="text-on-surface-variant block mb-1">TOOL_ARGS</span>
              <pre className="text-on-surface bg-surface-container p-2 overflow-x-auto max-h-32 overflow-y-auto whitespace-pre-wrap break-all">
                {formatJSON(event.tool_args)}
              </pre>
            </div>
            {event.response_payload && (
              <div>
                <span className="text-on-surface-variant block mb-1">RESPONSE_PAYLOAD</span>
                <pre className="text-on-surface bg-surface-container p-2 overflow-x-auto max-h-32 overflow-y-auto whitespace-pre-wrap break-all">
                  {formatJSON(event.response_payload)}
                </pre>
              </div>
            )}
            <div>
              <span className="text-on-surface-variant block mb-1">DETECTION_RESULTS</span>
              <pre className="text-on-surface bg-surface-container p-2 overflow-x-auto max-h-32 overflow-y-auto whitespace-pre-wrap break-all">
                {formatJSON(event.detection_results)}
              </pre>
            </div>
          </div>
        </div>
      )}

      <div className="mt-auto p-8 pt-0 flex gap-4">
        <button
          onClick={() => setShowReplay(!showReplay)}
          className={`px-6 py-3 font-mono font-bold text-sm flex items-center gap-3 transition-all ${
            showReplay
              ? "bg-primary/20 text-primary border border-primary/30"
              : "bg-primary text-on-primary hover:shadow-[0_0_20px_rgba(142,255,113,0.3)]"
          }`}
        >
          <span className="material-symbols-outlined">{showReplay ? "visibility_off" : "play_circle"}</span>
          {showReplay ? "HIDE_REPLAY" : "REPLAY_ATTACK"}
        </button>
        <button
          onClick={() => downloadEventReport(event)}
          className="bg-surface-container-highest text-on-surface px-6 py-3 font-mono font-bold text-sm border border-outline-variant/30 hover:border-primary/50 transition-all"
        >
          GENERATE_REPORT
        </button>
      </div>
    </div>
  );
}
