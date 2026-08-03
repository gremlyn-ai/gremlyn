"use client";

import { useState } from "react";
import { Sidebar } from "@/components/Sidebar";
import { TopBar } from "@/components/TopBar";
import { useShieldStatus, useShieldRules, useShieldServers } from "@/lib/hooks/useShieldData";
import { useArenaStatus } from "@/lib/hooks/useArenaData";

const SHIELD_URL = process.env.NEXT_PUBLIC_SHIELD_URL ?? "http://localhost:8081";
const ARENA_URL = process.env.NEXT_PUBLIC_ARENA_URL ?? "http://localhost:8082";

type ConnectionTest = { status: "idle" | "testing" | "ok" | "error"; latency?: number };

export default function SettingsPage() {
  const { data: shieldStatus, error: shieldError } = useShieldStatus();
  const { data: arenaStatus, error: arenaError } = useArenaStatus();
  const { data: rules } = useShieldRules();
  const { data: servers } = useShieldServers();

  const [shieldTest, setShieldTest] = useState<ConnectionTest>({ status: "idle" });
  const [arenaTest, setArenaTest] = useState<ConnectionTest>({ status: "idle" });

  async function testConnection(url: string, setter: (t: ConnectionTest) => void) {
    setter({ status: "testing" });
    const start = performance.now();
    try {
      await fetch(url, { mode: "cors" });
      const latency = Math.round(performance.now() - start);
      setter({ status: "ok", latency });
    } catch {
      setter({ status: "error" });
    }
  }

  function testBadge(test: ConnectionTest) {
    switch (test.status) {
      case "testing":
        return <span className="text-secondary animate-pulse">TESTING...</span>;
      case "ok":
        return <span className="text-primary">CONNECTED ({test.latency}ms)</span>;
      case "error":
        return <span className="text-error">UNREACHABLE</span>;
      default:
        return null;
    }
  }

  return (
    <div>
      <Sidebar activePage="settings" />

      <main className="flex-1 ml-64 bg-background relative min-h-screen">
        <TopBar accent="primary" statusText="Settings" statusLabel="Settings" />

        <div className="p-8 max-w-5xl mx-auto space-y-8 pb-24">
          {/* Page header */}
          <div className="border-b border-outline-variant/20 pb-4">
            <h3 className="text-4xl font-headline font-bold tracking-tight text-on-surface">
              SETTINGS
            </h3>
            <p className="text-on-surface-variant font-mono text-sm mt-1 uppercase tracking-tighter">
              System configuration and API connections
            </p>
          </div>

          {/* API Connections */}
          <div className="bg-surface-container-low p-6 space-y-6">
            <h4 className="font-headline text-sm font-bold uppercase tracking-widest text-primary">
              API_CONNECTIONS
            </h4>

            {/* Shield API */}
            <div className="bg-surface-container-lowest p-4 space-y-3">
              <div className="flex items-center justify-between">
                <div>
                  <span className="font-mono text-xs text-on-surface font-bold block">SHIELD_API</span>
                  <span className="font-mono text-[10px] text-on-surface-variant">{SHIELD_URL}</span>
                </div>
                <div className="flex items-center gap-3">
                  <span className={`w-2 h-2 ${shieldError ? "bg-error" : "bg-primary animate-pulse"}`} />
                  <span className="font-mono text-[10px]">
                    {shieldError ? (
                      <span className="text-error">OFFLINE</span>
                    ) : (
                      <span className="text-primary">ONLINE</span>
                    )}
                  </span>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <button
                  onClick={() => testConnection(SHIELD_URL + "/api/v1/status", setShieldTest)}
                  disabled={shieldTest.status === "testing"}
                  className="bg-surface-container-highest px-4 py-2 font-mono text-[10px] text-on-surface-variant hover:text-primary border border-outline-variant/20 hover:border-primary/30 transition-all disabled:opacity-40"
                >
                  TEST_CONNECTION
                </button>
                <span className="font-mono text-[10px]">{testBadge(shieldTest)}</span>
              </div>
              {shieldStatus && (
                <div className="grid grid-cols-3 gap-4 font-mono text-[10px] pt-2 border-t border-outline-variant/10">
                  <div>
                    <span className="text-on-surface-variant block">STATUS</span>
                    <span className="text-primary font-bold">{shieldStatus.running ? "RUNNING" : "STOPPED"}</span>
                  </div>
                  <div>
                    <span className="text-on-surface-variant block">UPTIME</span>
                    <span className="text-on-surface">{shieldStatus.uptime ?? "—"}</span>
                  </div>
                  <div>
                    <span className="text-on-surface-variant block">SERVERS</span>
                    <span className="text-on-surface">{shieldStatus.servers?.length ?? 0}</span>
                  </div>
                </div>
              )}
            </div>

            {/* Arena API */}
            <div className="bg-surface-container-lowest p-4 space-y-3">
              <div className="flex items-center justify-between">
                <div>
                  <span className="font-mono text-xs text-on-surface font-bold block">ARENA_API</span>
                  <span className="font-mono text-[10px] text-on-surface-variant">{ARENA_URL}</span>
                </div>
                <div className="flex items-center gap-3">
                  <span className={`w-2 h-2 ${arenaError ? "bg-error" : "bg-primary animate-pulse"}`} />
                  <span className="font-mono text-[10px]">
                    {arenaError ? (
                      <span className="text-error">OFFLINE</span>
                    ) : (
                      <span className="text-primary">ONLINE</span>
                    )}
                  </span>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <button
                  onClick={() => testConnection(ARENA_URL + "/status", setArenaTest)}
                  disabled={arenaTest.status === "testing"}
                  className="bg-surface-container-highest px-4 py-2 font-mono text-[10px] text-on-surface-variant hover:text-primary border border-outline-variant/20 hover:border-primary/30 transition-all disabled:opacity-40"
                >
                  TEST_CONNECTION
                </button>
                <span className="font-mono text-[10px]">{testBadge(arenaTest)}</span>
              </div>
              {arenaStatus && (
                <div className="grid grid-cols-3 gap-4 font-mono text-[10px] pt-2 border-t border-outline-variant/10">
                  <div>
                    <span className="text-on-surface-variant block">STATUS</span>
                    <span className="text-primary font-bold">{arenaStatus.status?.toUpperCase() ?? "—"}</span>
                  </div>
                  <div>
                    <span className="text-on-surface-variant block">ACTIVE_SESSIONS</span>
                    <span className="text-on-surface">{arenaStatus.active_sessions}</span>
                  </div>
                  <div>
                    <span className="text-on-surface-variant block">GREMLINS</span>
                    <span className="text-on-surface">{arenaStatus.gremlins_available}</span>
                  </div>
                </div>
              )}
            </div>
          </div>

          {/* System Info */}
          <div className="bg-surface-container-low p-6 space-y-4">
            <h4 className="font-headline text-sm font-bold uppercase tracking-widest text-primary">
              SYSTEM_INFO
            </h4>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              <div className="bg-surface-container-lowest p-4">
                <span className="font-mono text-[10px] text-on-surface-variant block mb-1">VERSION</span>
                <span className="font-mono text-xs text-on-surface font-bold">beta</span>
              </div>
              <div className="bg-surface-container-lowest p-4">
                <span className="font-mono text-[10px] text-on-surface-variant block mb-1">SERVERS</span>
                <span className="font-mono text-xs text-primary font-bold">{servers?.length ?? 0}</span>
              </div>
              <div className="bg-surface-container-lowest p-4">
                <span className="font-mono text-[10px] text-on-surface-variant block mb-1">RULES</span>
                <span className="font-mono text-xs text-primary font-bold">{rules?.length ?? 0}</span>
              </div>
              <div className="bg-surface-container-lowest p-4">
                <span className="font-mono text-[10px] text-on-surface-variant block mb-1">UPTIME</span>
                <span className="font-mono text-xs text-on-surface font-bold">{shieldStatus?.uptime ?? "—"}</span>
              </div>
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}
