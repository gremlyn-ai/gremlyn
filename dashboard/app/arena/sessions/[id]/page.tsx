"use client";

import { use, useState } from "react";
import Link from "next/link";
import { Sidebar } from "@/components/Sidebar";
import { TopBar } from "@/components/TopBar";
import { useArenaSession, useSessionEvents } from "@/lib/hooks/useArenaData";
import { stopSession } from "@/lib/api/arena";
import type { ArenaEvent, SessionConfig, ResilienceReport } from "@/lib/api/types";

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

function outcomeColor(outcome: string): string {
  switch (outcome) {
    case "survived": return "text-primary";
    case "crashed": return "text-error";
    case "degraded": return "text-secondary";
    default: return "text-on-surface-variant";
  }
}

function outcomeBg(outcome: string): string {
  switch (outcome) {
    case "survived": return "bg-primary/10 border-primary/20";
    case "crashed": return "bg-error/10 border-error/20";
    case "degraded": return "bg-secondary/10 border-secondary/20";
    default: return "bg-surface-container-high border-outline-variant/20";
  }
}

function formatTime(iso: string): string {
  return new Date(iso).toLocaleTimeString("en-US", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    fractionalSecondDigits: 3,
  });
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
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

function formatJSON(data: unknown): string {
  if (!data) return "—";
  try {
    return JSON.stringify(data, null, 2);
  } catch {
    return String(data);
  }
}

type PageProps = {
  params: Promise<{ id: string }>;
};

export default function SessionDetailPage({ params }: PageProps) {
  const { id } = use(params);
  const { data: session, error: sessionError, mutate: mutateSession } = useArenaSession(id);
  const { data: events } = useSessionEvents(id);
  const [stopping, setStopping] = useState(false);

  const isRunning = session?.status === "running";
  const config = session?.config as SessionConfig | undefined;
  const results = session?.results as ResilienceReport | undefined;

  async function handleStop() {
    setStopping(true);
    try {
      await stopSession(id);
      mutateSession();
    } catch {
      // Session may have already completed
    } finally {
      setStopping(false);
    }
  }

  return (
    <div className="selection-secondary">
      <div className="fixed inset-0 scanline-arena z-50 pointer-events-none opacity-5" />

      <Sidebar activePage="arena" />

      <main className="flex-1 ml-64 bg-background relative min-h-screen">
        <TopBar accent="secondary" statusText="Session_Detail" statusLabel="Session_Detail" activeTab="vitals" />

        <div className="p-8 max-w-7xl mx-auto space-y-8 pb-24 relative z-10">
          {/* Breadcrumb */}
          <div className="flex items-center gap-2 font-mono text-[10px] text-on-surface-variant">
            <Link href="/arena" className="hover:text-secondary">ARENA</Link>
            <span>/</span>
            <Link href="/arena/sessions" className="hover:text-secondary">SESSIONS</Link>
            <span>/</span>
            <span className="text-on-surface">{id.slice(0, 8)}</span>
          </div>

          {/* Error */}
          {sessionError && (
            <div className="bg-error/10 border border-error/20 p-4">
              <span className="font-mono text-xs text-error">[ERROR] Session not found or API unreachable</span>
            </div>
          )}

          {/* Loading */}
          {!session && !sessionError && (
            <div className="space-y-4">
              <div className="bg-surface-container-low p-8 animate-pulse">
                <div className="h-6 w-64 bg-surface-container-highest mb-4" />
                <div className="h-4 w-full bg-surface-container-highest" />
              </div>
            </div>
          )}

          {session && (
            <>
              {/* Session header */}
              <div className="flex items-end justify-between border-b border-outline-variant/20 pb-4">
                <div>
                  <div className="flex items-center gap-3 mb-2">
                    <span className={`font-mono text-xs px-2 py-0.5 border ${statusBadge(session.status)}`}>
                      {session.status.toUpperCase()}
                    </span>
                    {isRunning && (
                      <span className="flex items-center gap-1.5 font-mono text-[10px] text-primary">
                        <span className="w-1.5 h-1.5 bg-primary rounded-full animate-pulse" />
                        LIVE
                      </span>
                    )}
                  </div>
                  <h3 className="text-3xl font-headline font-bold tracking-tight">
                    SESSION {id.slice(0, 8)}
                  </h3>
                </div>
                <div className="flex items-center gap-4">
                  {isRunning && (
                    <button
                      onClick={handleStop}
                      disabled={stopping}
                      className="px-4 py-2 bg-error/10 border border-error/20 text-error font-mono text-xs font-bold tracking-widest uppercase hover:bg-error hover:text-on-primary transition-all disabled:opacity-40"
                    >
                      {stopping ? "STOPPING..." : "STOP_SESSION"}
                    </button>
                  )}
                  <Link
                    href="/arena/sessions"
                    className="font-mono text-[10px] text-secondary hover:underline"
                  >
                    BACK_TO_LIST
                  </Link>
                </div>
              </div>

              {/* Session overview cards */}
              <div className="grid grid-cols-12 gap-6">
                {/* Config card */}
                <div className="col-span-12 lg:col-span-6">
                  <div className="bg-surface-container-low p-6 space-y-4 h-full">
                    <h4 className="font-headline text-sm font-bold uppercase tracking-widest text-secondary">
                      SESSION_CONFIG
                    </h4>
                    <div className="grid grid-cols-2 gap-4 font-mono text-xs">
                      <div>
                        <span className="text-on-surface-variant block mb-1">SERVER_ID</span>
                        <span className="text-on-surface uppercase">{session.server_id}</span>
                      </div>
                      <div>
                        <span className="text-on-surface-variant block mb-1">STARTED_AT</span>
                        <span className="text-on-surface">{formatDate(session.started_at)}</span>
                      </div>
                      <div>
                        <span className="text-on-surface-variant block mb-1">DURATION</span>
                        <span className="text-on-surface">{duration(session.started_at, session.completed_at)}</span>
                      </div>
                      <div>
                        <span className="text-on-surface-variant block mb-1">INTENSITY</span>
                        <span className="text-secondary uppercase">{config?.intensity ?? "—"}</span>
                      </div>
                      <div className="col-span-2">
                        <span className="text-on-surface-variant block mb-1">GREMLINS</span>
                        <div className="flex flex-wrap gap-2">
                          {config?.gremlins?.map((g) => (
                            <span key={g} className="px-2 py-0.5 bg-secondary/10 border border-secondary/20 text-secondary text-[10px] uppercase">
                              {g}
                            </span>
                          )) ?? <span className="text-on-surface-variant">—</span>}
                        </div>
                      </div>
                      {config?.prompts && config.prompts.length > 0 && (
                        <div className="col-span-2">
                          <span className="text-on-surface-variant block mb-1">CUSTOM_PROMPTS</span>
                          <div className="bg-surface-container-lowest p-3 max-h-32 overflow-y-auto">
                            {config.prompts.map((p, i) => (
                              <div key={i} className="text-on-surface-variant py-0.5">{p}</div>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                </div>

                {/* Results card */}
                <div className="col-span-12 lg:col-span-6">
                  <div className="bg-surface-container-low p-6 space-y-4 h-full">
                    <h4 className="font-headline text-sm font-bold uppercase tracking-widest text-secondary">
                      RESULTS
                    </h4>
                    <div className="grid grid-cols-3 gap-4 font-mono text-xs">
                      <div className="bg-surface-container-lowest p-4 text-center">
                        <span className="text-3xl font-bold text-on-surface block">{session.gremlins_sent}</span>
                        <span className="text-on-surface-variant text-[10px]">SENT</span>
                      </div>
                      <div className="bg-surface-container-lowest p-4 text-center">
                        <span className="text-3xl font-bold text-primary block">{session.gremlins_survived}</span>
                        <span className="text-on-surface-variant text-[10px]">SURVIVED</span>
                      </div>
                      <div className="bg-surface-container-lowest p-4 text-center">
                        <span className="text-3xl font-bold text-error block">{session.gremlins_crashed}</span>
                        <span className="text-on-surface-variant text-[10px]">CRASHED</span>
                      </div>
                    </div>

                    {results && (
                      <div className="mt-4 space-y-3">
                        <div className="flex items-center justify-between">
                          <span className="text-on-surface-variant font-mono text-xs">OVERALL_SCORE</span>
                          <span className="font-headline text-2xl font-bold text-primary">{results.overall}</span>
                        </div>
                        <div className="flex items-center justify-between">
                          <span className="text-on-surface-variant font-mono text-xs">GRADE</span>
                          <span className={`font-headline text-lg font-bold uppercase ${
                            results.grade === "excellent" ? "text-primary" :
                            results.grade === "good" ? "text-tertiary" :
                            results.grade === "needs_work" ? "text-secondary" : "text-error"
                          }`}>{results.grade?.replace("_", " ") ?? "—"}</span>
                        </div>

                        {results.dimensions && Object.entries(results.dimensions).map(([name, dim]) => {
                          const pct = dim.total > 0 ? Math.min((dim.score / dim.total) * 100, 100) : 0;
                          const isOverflow = dim.total > 0 && dim.score > dim.total;
                          return (
                            <div key={name} className="bg-surface-container-lowest p-3">
                              <div className="flex justify-between items-center mb-1">
                                <span className="font-mono text-[10px] text-on-surface-variant uppercase">{name}</span>
                                <span className={`font-mono text-xs ${isOverflow ? "text-error" : "text-on-surface"}`}>
                                  {dim.score}/{dim.total}
                                </span>
                              </div>
                              <div className="h-1 bg-surface-container-highest">
                                <div
                                  className={`h-full transition-all ${isOverflow ? "bg-error" : "bg-primary"}`}
                                  style={{ width: `${pct}%` }}
                                />
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    )}

                    {!results && !isRunning && (
                      <div className="text-on-surface-variant font-mono text-xs text-center py-4">
                        NO_RESULTS_AVAILABLE
                      </div>
                    )}
                    {isRunning && (
                      <div className="text-primary font-mono text-xs text-center py-4 animate-pulse">
                        COMPUTING_RESULTS...
                      </div>
                    )}
                  </div>
                </div>
              </div>

              {/* Event trace */}
              <div className="bg-surface-container-low p-6">
                <div className="flex justify-between items-center mb-6">
                  <h4 className="font-headline text-sm font-bold uppercase tracking-widest text-secondary">
                    EVENT_TRACE
                  </h4>
                  <span className="font-mono text-[10px] text-on-surface-variant">
                    {events?.length ?? 0} EVENTS
                  </span>
                </div>

                {(!events || events.length === 0) && (
                  <div className="text-on-surface-variant font-mono text-xs text-center py-8">
                    {isRunning ? "WAITING_FOR_EVENTS..." : "NO_EVENTS_RECORDED"}
                  </div>
                )}

                {events && events.length > 0 && (
                  <div className="space-y-3">
                    {events.map((e: ArenaEvent, i: number) => (
                      <div
                        key={e.id}
                        className="bg-surface-container-lowest border-l-2 border-outline-variant/20 hover:border-secondary transition-colors"
                      >
                        {/* Event header */}
                        <div className="flex items-center gap-4 px-4 py-3 font-mono text-xs">
                          <span className="text-on-surface-variant w-6 text-right shrink-0">
                            #{i + 1}
                          </span>
                          <span className="text-on-surface-variant w-24 shrink-0">
                            {formatTime(e.injected_at)}
                          </span>
                          <span className="text-secondary w-28 shrink-0 uppercase font-bold">
                            {e.gremlin_type}
                          </span>
                          <span className={`px-2 py-0.5 text-[10px] border uppercase ${outcomeBg(e.outcome)} ${outcomeColor(e.outcome)}`}>
                            {e.outcome}
                          </span>
                          <span className="text-on-surface-variant ml-auto">
                            score: <span className="text-on-surface font-bold">{e.score}</span>
                          </span>
                        </div>

                        {/* Event details */}
                        <div className="px-4 pb-3 grid grid-cols-12 gap-4 font-mono text-[10px]">
                          <div className="col-span-4">
                            <span className="text-on-surface-variant block mb-1">GREMLIN_CONFIG</span>
                            <pre className="text-on-surface bg-surface-container p-2 overflow-x-auto max-h-24 overflow-y-auto whitespace-pre-wrap break-all">
                              {formatJSON(e.gremlin_config)}
                            </pre>
                          </div>
                          <div className="col-span-4">
                            <span className="text-on-surface-variant block mb-1">AGENT_RESPONSE</span>
                            <pre className="text-on-surface bg-surface-container p-2 overflow-x-auto max-h-24 overflow-y-auto whitespace-pre-wrap break-all">
                              {formatJSON(e.agent_response)}
                            </pre>
                          </div>
                          <div className="col-span-4">
                            <span className="text-on-surface-variant block mb-1">DETAILS</span>
                            <pre className="text-on-surface bg-surface-container p-2 overflow-x-auto max-h-24 overflow-y-auto whitespace-pre-wrap break-all">
                              {formatJSON(e.details)}
                            </pre>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </>
          )}
        </div>
      </main>
    </div>
  );
}
