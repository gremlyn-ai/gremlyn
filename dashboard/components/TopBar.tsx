"use client";

import Link from "next/link";
import { useTerminalStore } from "@/lib/stores/terminalStore";

type TopBarProps = {
  accent: "primary" | "secondary";
  statusText: string;
  statusLabel: string;
  activeTab?: "vitals" | "logs" | "matrix";
  nodeCount?: number;
  isOnline?: boolean;
};

const ARENA_TABS: { key: "vitals" | "logs" | "matrix"; label: string; href: string | null }[] = [
  { key: "vitals", label: "Vitals", href: "/arena" },
  { key: "logs", label: "Logs", href: "/arena/sessions" },
  { key: "matrix", label: "Matrix", href: null },
];

export function TopBar({ accent, statusText, statusLabel, activeTab, nodeCount, isOnline }: TopBarProps) {
  const accentHex = accent === "secondary" ? "#ff7168" : "#8eff71";
  const isArena = accent === "secondary";

  return (
    <header className="flex justify-between items-center w-full px-8 h-16 bg-[#0e0e0e] border-b border-outline-variant/5 sticky top-0 z-30">
      <div className="flex items-center gap-8">
        <div className="flex items-center gap-3">
          <span
            className="w-2 h-2 animate-pulse"
            style={{ backgroundColor: accentHex }}
          />
          <h2
            className="font-headline font-black tracking-tighter text-xl uppercase"
            style={{ color: accentHex }}
          >
            GREMLYN_OS
          </h2>
        </div>

        {isArena ? (
          <nav className="hidden md:flex items-center gap-6">
            {ARENA_TABS.map((tab) => {
              const isActive = activeTab === tab.key;
              if (!tab.href) {
                return (
                  <span
                    key={tab.key}
                    className="font-headline uppercase tracking-widest text-xs text-[#262626] h-16 flex items-center cursor-not-allowed"
                    title="COMING_SOON"
                  >
                    {tab.label}
                  </span>
                );
              }
              return (
                <Link
                  key={tab.key}
                  href={tab.href}
                  className={`font-headline uppercase tracking-widest text-xs h-16 flex items-center transition-colors ${
                    isActive
                      ? "text-secondary border-b-2 border-secondary"
                      : "text-[#262626] hover:text-primary"
                  }`}
                >
                  {tab.label}
                </Link>
              );
            })}
          </nav>
        ) : (
          <div className="hidden md:flex items-center gap-6 font-headline uppercase tracking-widest text-sm">
            <span className="flex items-center gap-2 text-primary">
              <span className={`w-2 h-2 ${isOnline !== false ? "bg-primary animate-pulse" : "bg-error"}`} />
              {isOnline !== false ? "Shield_Active" : "Shield_Offline"}
            </span>
            <span className="text-on-surface-variant">
              Vitals: <span className="text-on-surface">{isOnline !== false ? "Stable" : "Degraded"}</span>
            </span>
            <span className="text-on-surface-variant">
              Nodes: <span className="text-on-surface">{nodeCount ?? "—"}</span>
            </span>
          </div>
        )}
      </div>

      <div className="flex items-center gap-4">
        {isArena ? (
          <div className="flex items-center gap-4 px-4 py-1.5 bg-surface-container-low border border-outline-variant/10">
            <div className="flex flex-col">
              <span className="font-mono text-[10px] text-on-surface-variant leading-none">
                STATUS
              </span>
              <span className="font-mono text-xs font-bold text-secondary uppercase">
                {statusLabel}
              </span>
            </div>
          </div>
        ) : (
          <div className="bg-surface-container-low px-4 py-1.5 flex items-center gap-3 border border-outline-variant/20">
            <span className="material-symbols-outlined text-primary text-sm">terminal</span>
            <span className="font-mono text-[10px] text-on-surface-variant">
              USER@ROOT_ACCESS: OK
            </span>
          </div>
        )}

        <div className="flex items-center gap-2">
          <button
            onClick={() => useTerminalStore.getState().toggle()}
            className="p-2 text-on-surface-variant hover:text-primary hover:bg-surface-container-low transition-all"
            title="TOGGLE_TERMINAL"
          >
            <span className="material-symbols-outlined">terminal</span>
          </button>
        </div>
      </div>
    </header>
  );
}
