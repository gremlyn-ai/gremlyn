"use client";

import { useState, useEffect, useRef } from "react";
import Link from "next/link";
import { Sidebar } from "@/components/Sidebar";
import { TopBar } from "@/components/TopBar";
import { GremlinSelector } from "./components/GremlinSelector";
import { ArenaControls } from "./components/ArenaControls";
import { TerminalPanel } from "./components/TerminalPanel";
import { useArenaGremlins, useArenaStatus, useArenaSession, useSessionEvents } from "@/lib/hooks/useArenaData";
import { useShieldStatus } from "@/lib/hooks/useShieldData";
import { useArenaWebSocket } from "@/lib/hooks/useArenaWebSocket";
import { useArenaStore } from "@/lib/stores/arenaStore";
import { createSession, stopSession } from "@/lib/api/arena";
import type { ArenaEvent } from "@/lib/api/types";

function intensityLabel(n: number): string {
  if (n <= 3) return "low";
  if (n <= 7) return "medium";
  return "high";
}

function outcomeColor(outcome: string): string {
  switch (outcome) {
    case "survived": return "text-primary";
    case "crashed": return "text-error";
    case "degraded": return "text-secondary";
    default: return "text-on-surface-variant";
  }
}

export default function ArenaPage() {
  const { data: arenaStatus, error: arenaError } = useArenaStatus();
  const { data: gremlins } = useArenaGremlins();
  const { data: shieldStatus } = useShieldStatus();

  const currentSession = useArenaStore((s) => s.currentSession);
  const setSession = useArenaStore((s) => s.setSession);
  const setError = useArenaStore((s) => s.setError);
  const error = useArenaStore((s) => s.error);
  const events = useArenaStore((s) => s.events);
  const clearEvents = useArenaStore((s) => s.clearEvents);
  const showTerminal = useArenaStore((s) => s.showTerminal);

  const [selectedGremlins, setSelectedGremlins] = useState<string[]>([]);
  const [selectedServer, setSelectedServer] = useState("");
  const [intensity, setIntensity] = useState(5);
  const [prompts, setPrompts] = useState("");
  const [launching, setLaunching] = useState(false);

  const servers = shieldStatus?.servers ?? [];

  // Poll session status while it's running or just launched.
  const sessionId = currentSession?.id ?? null;
  const isRunning = currentSession?.status === "running";
  const shouldPoll = sessionId !== null;
  useArenaSession(shouldPoll ? sessionId : null);

  // Fetch events from REST API (fallback when WS misses events or session already done).
  const { data: restEvents } = useSessionEvents(sessionId);
  const addEvent = useArenaStore((s) => s.addEvent);

  // Merge REST events into store when WS missed them.
  const mergedRef = useRef(false);
  useEffect(() => {
    if (restEvents && restEvents.length > 0 && events.length === 0 && !mergedRef.current) {
      mergedRef.current = true;
      for (const e of restEvents) {
        addEvent(e);
      }
    }
  }, [restEvents, events.length, addEvent]);

  // Reset merge flag on new session.
  useEffect(() => {
    mergedRef.current = false;
  }, [sessionId]);

  // WebSocket for live events.
  const { connect, disconnect } = useArenaWebSocket(sessionId);
  const connectedRef = useRef(false);

  useEffect(() => {
    if (sessionId && isRunning && !connectedRef.current) {
      connect();
      connectedRef.current = true;
    }
    if (!isRunning && connectedRef.current) {
      disconnect();
      connectedRef.current = false;
    }
    return () => {
      if (connectedRef.current) {
        disconnect();
        connectedRef.current = false;
      }
    };
  }, [sessionId, isRunning, connect, disconnect]);

  // Auto-scroll event log.
  const logRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight;
    }
  }, [events]);

  async function handleLaunch() {
    if (!selectedServer || selectedGremlins.length === 0) return;
    setLaunching(true);
    setError(null);
    clearEvents();
    try {
      const promptList = prompts
        .split("\n")
        .map((l) => l.trim())
        .filter(Boolean);
      const session = await createSession(
        selectedServer,
        selectedGremlins,
        intensityLabel(intensity),
        promptList.length > 0 ? promptList : undefined
      );
      setSession(session);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to launch session");
    } finally {
      setLaunching(false);
    }
  }

  const displaySessionId = currentSession?.id ?? "AWAITING";
  const canLaunch = selectedServer.length > 0 && selectedGremlins.length > 0 && !launching;

  return (
    <div className="selection-secondary">
      <div className="fixed inset-0 scanline-arena z-50 pointer-events-none opacity-5" />

      <Sidebar activePage="arena" />

      <main className="flex-1 ml-64 bg-background relative min-h-screen">
        <TopBar accent="secondary" statusText="Arena_Live" statusLabel="Arena_Live" activeTab="vitals" />

        <div className="p-8 max-w-7xl mx-auto space-y-12 pb-24 relative z-10">
          {/* Error banners */}
          {arenaError && (
            <div className="bg-error/10 border border-error/20 p-4">
              <span className="font-mono text-xs text-error">[ERROR] Arena API unreachable</span>
            </div>
          )}
          {error && (
            <div className="bg-error/10 border border-error/20 p-4 flex justify-between items-center">
              <span className="font-mono text-xs text-error">[ERROR] {error}</span>
              <button onClick={() => setError(null)} className="text-error font-mono text-xs hover:bg-error/10 px-2 py-1 transition-all">
                DISMISS
              </button>
            </div>
          )}

          {/* Section Header */}
          <div className="flex items-end justify-between border-b border-outline-variant/20 pb-4">
            <div>
              <span className="font-mono text-secondary text-xs font-bold tracking-[0.2em]">
                TAB_01
              </span>
              <h3 className="text-4xl font-headline font-bold tracking-tight mt-1">
                NEW SESSION
              </h3>
            </div>
            <div className="flex items-center gap-4 text-on-surface-variant font-mono text-[10px]">
              <Link
                href="/arena/sessions"
                className="text-secondary hover:underline"
              >
                VIEW_ALL_SESSIONS
              </Link>
              <span>
                SESSION_ID: <span className="text-on-surface">{displaySessionId}</span>
              </span>
              <span>
                ACTIVE: <span className="text-primary">{arenaStatus?.active_sessions ?? 0}</span>
              </span>
            </div>
          </div>

          {/* Bento Grid */}
          <div className="grid grid-cols-12 gap-6">
            <GremlinSelector
              gremlins={gremlins ?? []}
              selected={selectedGremlins}
              onSelectionChange={setSelectedGremlins}
            />
            <ArenaControls
              servers={servers}
              selectedServer={selectedServer}
              onServerChange={setSelectedServer}
              intensity={intensity}
              onIntensityChange={setIntensity}
            />

            {/* Prompt Injection textarea */}
            <div className="col-span-12">
              <div className="bg-surface-container-low p-8 space-y-4">
                <label className="font-headline text-sm font-bold uppercase tracking-widest text-secondary block">
                  Target Prompt Injection
                </label>
                <div className="relative">
                  <textarea
                    className="w-full bg-surface-container-lowest border-0 text-primary font-mono text-xs p-6 placeholder:text-on-surface-variant/30 focus:ring-1 focus:ring-secondary resize-none"
                    placeholder="Enter test injection sequences or raw system instructions here..."
                    rows={6}
                    value={prompts}
                    onChange={(e) => setPrompts(e.target.value)}
                  />
                  <div className="absolute bottom-4 right-4 flex gap-2">
                    <button
                      onClick={() => setPrompts("")}
                      className="bg-surface-container-high px-3 py-1 text-[10px] font-mono hover:bg-surface-container-highest hover:text-primary transition-all"
                    >
                      CLEAR
                    </button>
                  </div>
                </div>
              </div>
            </div>

            {/* Launch Chaos button */}
            <div className="col-span-12 pt-6">
              <button
                onClick={handleLaunch}
                disabled={!canLaunch}
                className={`w-full py-10 relative group overflow-hidden transition-all ${
                  canLaunch
                    ? "bg-secondary text-on-secondary cursor-pointer hover:shadow-[0_0_20px_rgba(255,113,104,0.4)]"
                    : "bg-surface-container-highest text-on-surface-variant cursor-not-allowed"
                }`}
              >
                <div className="absolute inset-0 bg-white/10 opacity-0 group-hover:opacity-100 transition-opacity" />
                <div className="relative flex flex-col items-center gap-2">
                  <span className="font-headline text-3xl font-black tracking-[0.2em]">
                    {launching ? "LAUNCHING..." : "LAUNCH CHAOS"}
                  </span>
                  <span className="font-mono text-xs font-bold opacity-70">
                    {selectedServer
                      ? `EXERTING CONTROL OVER [${selectedServer.toUpperCase()}]`
                      : "SELECT A TARGET TO BEGIN"}
                  </span>
                </div>
                <div className="absolute top-0 left-0 w-8 h-8 border-t-2 border-l-2 border-on-secondary/30" />
                <div className="absolute bottom-0 right-0 w-8 h-8 border-b-2 border-r-2 border-on-secondary/30" />
              </button>
            </div>

            {/* Session result + live events */}
            {currentSession && (
              <>
                <div className="col-span-12">
                  <Link
                    href={`/arena/sessions/${currentSession.id}`}
                    className="block bg-surface-container-low border-l-2 border-primary p-6 hover:bg-surface-container-high transition-colors cursor-pointer group"
                  >
                    <div className="flex justify-between items-center mb-4">
                      <h4 className="font-headline font-bold uppercase text-primary">
                        SESSION_ACTIVE
                      </h4>
                      <div className="flex items-center gap-3">
                        {isRunning && (
                          <span className="flex items-center gap-1.5 font-mono text-[10px] text-primary">
                            <span className="w-1.5 h-1.5 bg-primary rounded-full animate-pulse" />
                            LIVE
                          </span>
                        )}
                        <span className={`font-mono text-xs px-2 py-0.5 ${
                          currentSession.status === "running"
                            ? "bg-primary/10 text-primary border border-primary/20"
                            : currentSession.status === "completed"
                            ? "bg-tertiary/10 text-tertiary border border-tertiary/20"
                            : "bg-error/10 text-error border border-error/20"
                        }`}>
                          {currentSession.status.toUpperCase()}
                        </span>
                        <span className="font-mono text-xs px-3 py-1 bg-secondary/10 border border-secondary/30 text-secondary group-hover:bg-secondary group-hover:text-on-secondary transition-colors uppercase tracking-widest font-bold">
                          FULL_DETAILS
                        </span>
                      </div>
                    </div>
                    <div className="grid grid-cols-4 gap-4 font-mono text-xs">
                      <div>
                        <span className="text-on-surface-variant block mb-1">ID</span>
                        <span className="text-on-surface">{currentSession.id.slice(0, 8)}</span>
                      </div>
                      <div>
                        <span className="text-on-surface-variant block mb-1">SENT</span>
                        <span className="text-on-surface">{currentSession.gremlins_sent}</span>
                      </div>
                      <div>
                        <span className="text-on-surface-variant block mb-1">SURVIVED</span>
                        <span className="text-primary">{currentSession.gremlins_survived}</span>
                      </div>
                      <div>
                        <span className="text-on-surface-variant block mb-1">CRASHED</span>
                        <span className="text-error">{currentSession.gremlins_crashed}</span>
                      </div>
                    </div>
                  </Link>
                  {isRunning && (
                    <button
                      onClick={async (e) => {
                        e.preventDefault();
                        try {
                          await stopSession(currentSession.id);
                        } catch (err) {
                          setError(err instanceof Error ? err.message : "Failed to stop session");
                        }
                      }}
                      className="mt-2 w-full py-3 bg-error/10 border border-error/20 text-error font-mono text-xs font-bold tracking-widest uppercase hover:bg-error hover:text-on-primary transition-all"
                    >
                      STOP_SESSION
                    </button>
                  )}
                </div>

                {/* Live event log */}
                <div className="col-span-12">
                  <div className="bg-surface-container-low p-6">
                    <div className="flex justify-between items-center mb-4">
                      <h4 className="font-headline font-bold uppercase text-secondary text-sm tracking-widest">
                        EVENT_LOG
                      </h4>
                      <span className="font-mono text-[10px] text-on-surface-variant">
                        {events.length} EVENTS
                      </span>
                    </div>
                    <div
                      ref={logRef}
                      className="max-h-80 overflow-y-auto space-y-1 font-mono text-xs"
                    >
                      {events.length === 0 && (
                        <div className="text-on-surface-variant py-4 text-center">
                          {isRunning ? "WAITING_FOR_EVENTS..." : "NO_EVENTS_RECORDED"}
                        </div>
                      )}
                      {events.map((e: ArenaEvent) => (
                        <div
                          key={e.id}
                          className="flex items-center gap-3 py-1.5 px-3 hover:bg-surface-container-high transition-colors"
                        >
                          <span className="text-on-surface-variant w-20 shrink-0">
                            {new Date(e.injected_at).toLocaleTimeString()}
                          </span>
                          <span className="text-secondary w-28 shrink-0 uppercase">
                            {e.gremlin_type}
                          </span>
                          <span className={`w-20 shrink-0 uppercase ${outcomeColor(e.outcome)}`}>
                            {e.outcome}
                          </span>
                          <span className="text-on-surface-variant">
                            score: <span className="text-on-surface">{e.score}</span>
                          </span>
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              </>
            )}

            {/* Terminal debug panel */}
            {showTerminal && <TerminalPanel />}
          </div>
        </div>
      </main>
    </div>
  );
}
