"use client";

import { useEffect, useRef } from "react";
import { useArenaStore } from "@/lib/stores/arenaStore";
import type { ArenaEvent } from "@/lib/api/types";

function formatEventLine(e: ArenaEvent): string {
  const ts = new Date(e.injected_at).toISOString();
  const cfg = e.gremlin_config ? JSON.stringify(e.gremlin_config) : "{}";
  return `[${ts}] ${e.gremlin_type.toUpperCase()} → ${e.outcome.toUpperCase()} score=${e.score} config=${cfg}`;
}

export function TerminalPanel() {
  const events = useArenaStore((s) => s.events);
  const toggle = useArenaStore((s) => s.toggleTerminal);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [events]);

  return (
    <div className="col-span-12">
      <div className="bg-black border border-outline-variant/20">
        <div className="flex items-center justify-between px-4 py-2 border-b border-outline-variant/10">
          <div className="flex items-center gap-2">
            <span className="material-symbols-outlined text-primary text-sm">terminal</span>
            <span className="font-mono text-[10px] text-primary uppercase tracking-widest font-bold">
              WS_DEBUG_TERMINAL
            </span>
            <span className="font-mono text-[10px] text-on-surface-variant">
              {events.length} MSG
            </span>
          </div>
        </div>
        <div
          ref={scrollRef}
          className="max-h-48 overflow-y-auto p-4 font-mono text-[10px] leading-relaxed"
        >
          {events.length === 0 ? (
            <span className="text-on-surface-variant">$ AWAITING_WS_MESSAGES...</span>
          ) : (
            events.map((e) => (
              <div key={e.id} className="text-primary/80 hover:text-primary">
                <span className="text-on-surface-variant">$ </span>
                {formatEventLine(e)}
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
