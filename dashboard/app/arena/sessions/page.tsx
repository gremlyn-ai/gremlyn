"use client";

import Link from "next/link";
import { Sidebar } from "@/components/Sidebar";
import { TopBar } from "@/components/TopBar";
import { useArenaSessions } from "@/lib/hooks/useArenaData";
import type { ArenaSession } from "@/lib/api/types";

function statusBadge(status: string) {
  switch (status) {
    case "running":
      return "bg-primary/10 text-primary border-primary/20";
    case "completed":
      return "bg-tertiary/10 text-tertiary border-tertiary/20";
    case "cancelled":
      return "bg-error/10 text-error border-error/20";
    default:
      return "bg-surface-container-high text-on-surface-variant border-outline-variant/20";
  }
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function duration(start: string, end?: string | null): string {
  const s = new Date(start).getTime();
  const e = end ? new Date(end).getTime() : Date.now();
  const sec = Math.round((e - s) / 1000);
  if (sec < 60) return `${sec}s`;
  return `${Math.floor(sec / 60)}m ${sec % 60}s`;
}

export default function SessionsPage() {
  const { data: sessions, error, isLoading } = useArenaSessions();

  const sorted = [...(sessions ?? [])].sort(
    (a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime()
  );

  return (
    <div className="selection-secondary">
      <div className="fixed inset-0 scanline-arena z-50 pointer-events-none opacity-5" />

      <Sidebar activePage="arena" />

      <main className="flex-1 ml-64 bg-background relative min-h-screen">
        <TopBar accent="secondary" statusText="Arena_Sessions" statusLabel="Arena_Sessions" activeTab="logs" />

        <div className="p-8 max-w-7xl mx-auto space-y-8 pb-24 relative z-10">
          {/* Header */}
          <div className="flex items-end justify-between border-b border-outline-variant/20 pb-4">
            <div>
              <span className="font-mono text-secondary text-xs font-bold tracking-[0.2em]">
                HISTORY
              </span>
              <h3 className="text-4xl font-headline font-bold tracking-tight mt-1">
                ALL SESSIONS
              </h3>
            </div>
            <Link
              href="/arena"
              className="font-mono text-[10px] text-secondary hover:underline"
            >
              NEW_SESSION
            </Link>
          </div>

          {/* Error */}
          {error && (
            <div className="bg-error/10 border border-error/20 p-4">
              <span className="font-mono text-xs text-error">[ERROR] Failed to load sessions</span>
            </div>
          )}

          {/* Loading */}
          {isLoading && (
            <div className="space-y-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <div key={i} className="bg-surface-container-low p-6 animate-pulse">
                  <div className="h-4 w-48 bg-surface-container-highest mb-3" />
                  <div className="h-3 w-full bg-surface-container-highest" />
                </div>
              ))}
            </div>
          )}

          {/* Empty */}
          {!isLoading && sorted.length === 0 && (
            <div className="bg-surface-container-low p-12 text-center">
              <span className="material-symbols-outlined text-4xl text-on-surface-variant mb-4 block">
                bolt
              </span>
              <p className="font-mono text-sm text-on-surface-variant">NO_SESSIONS_FOUND</p>
              <Link href="/arena" className="font-mono text-xs text-secondary hover:underline mt-2 inline-block">
                LAUNCH_FIRST_SESSION
              </Link>
            </div>
          )}

          {/* Session table */}
          {sorted.length > 0 && (
            <div className="bg-surface-container-low">
              {/* Table header */}
              <div className="grid grid-cols-12 gap-4 px-6 py-3 border-b border-outline-variant/10 font-mono text-[10px] text-on-surface-variant uppercase">
                <div className="col-span-2">ID</div>
                <div className="col-span-2">SERVER</div>
                <div className="col-span-1">STATUS</div>
                <div className="col-span-2">STARTED</div>
                <div className="col-span-1">DURATION</div>
                <div className="col-span-1 text-center">SENT</div>
                <div className="col-span-1 text-center">SURVIVED</div>
                <div className="col-span-1 text-center">CRASHED</div>
                <div className="col-span-1"></div>
              </div>

              {/* Rows */}
              {sorted.map((s: ArenaSession) => (
                <Link
                  key={s.id}
                  href={`/arena/sessions/${s.id}`}
                  className="grid grid-cols-12 gap-4 px-6 py-4 border-b border-outline-variant/5 hover:bg-surface-container-high transition-colors font-mono text-xs items-center group"
                >
                  <div className="col-span-2 text-on-surface">{s.id.slice(0, 8)}</div>
                  <div className="col-span-2 text-on-surface-variant uppercase">{s.server_id}</div>
                  <div className="col-span-1">
                    <span className={`px-2 py-0.5 text-[10px] border ${statusBadge(s.status)}`}>
                      {s.status.toUpperCase()}
                    </span>
                  </div>
                  <div className="col-span-2 text-on-surface-variant">{formatDate(s.started_at)}</div>
                  <div className="col-span-1 text-on-surface-variant">{duration(s.started_at, s.completed_at)}</div>
                  <div className="col-span-1 text-center text-on-surface">{s.gremlins_sent}</div>
                  <div className="col-span-1 text-center text-primary">{s.gremlins_survived}</div>
                  <div className="col-span-1 text-center text-error">{s.gremlins_crashed}</div>
                  <div className="col-span-1 text-right">
                    <span className="text-on-surface-variant group-hover:text-secondary transition-colors material-symbols-outlined text-sm">
                      chevron_right
                    </span>
                  </div>
                </Link>
              ))}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}
