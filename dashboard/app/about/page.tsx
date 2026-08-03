"use client";

import { Sidebar } from "@/components/Sidebar";
import { TopBar } from "@/components/TopBar";

const FEATURES = [
  {
    icon: "shield",
    title: "GREMLYN_SHIELD",
    accent: "primary",
    description: "Real-time MCP firewall that intercepts, inspects, and enforces security policies on all tool calls between AI agents and MCP servers.",
    capabilities: [
      "Multi-layer detection: regex, ML classifier, LLM-as-judge, structural analysis",
      "Policy engine: block, redact, alert, throttle, pause, log actions",
      "PII redaction and cross-entity leak prevention",
      "Behavioral profiling and anomaly detection",
      "Real-time Slack, email, and webhook alerts",
    ],
  },
  {
    icon: "bolt",
    title: "GREMLYN_ARENA",
    accent: "secondary",
    description: "Chaos testing engine that deploys adversarial gremlins against AI agents to measure their resilience before production.",
    capabilities: [
      "8 gremlin types: hallucination, latency, corruption, loop, injection, identity, overflow, timeout",
      "Configurable intensity levels and targeted test prompts",
      "Real-time WebSocket event streaming during sessions",
      "Per-dimension resilience scoring with overall grade",
      "CI/CD integration with pass/fail exit codes",
    ],
  },
];

const USE_CASES = [
  {
    title: "ENTERPRISE_MCP_PROTECTION",
    description: "Shield blocks bulk data exports, detects cross-entity data leaks, and redacts PII from MCP server responses in real time.",
    example: "HubSpot MCP: block search_contacts with limit > 100, redact emails from Slack outgoing messages.",
  },
  {
    title: "CI_CD_PIPELINE",
    description: "Arena runs as part of GitHub Actions. gremlyn arena run --ci returns exit code based on resilience threshold.",
    example: "Build fails if overall resilience score < 70%. PR comments show score breakdown per dimension.",
  },
  {
    title: "COMPLIANCE_REPORTING",
    description: "Shield generates weekly reports for SOC 2, HIPAA, and GDPR audits showing requests proxied, attacks blocked, and rules triggered.",
    example: "Automated PDF export: total blocked by category, detection layer effectiveness, top triggered rules.",
  },
  {
    title: "INCIDENT_RESPONSE",
    description: "Shield detects prompt injection in MCP responses via 3-layer detection pipeline and blocks within milliseconds.",
    example: "Regex layer blocks in <1ms, ML classifier in ~10ms, LLM-as-judge in ~500ms. Slack alert fires immediately.",
  },
  {
    title: "BEHAVIORAL_MONITORING",
    description: "Shield builds behavioral fingerprints over time and alerts on anomalous tool call sequences or data exfiltration patterns.",
    example: "Agent suddenly calls file_write after 200 read-only calls. Shield flags the anomaly and pauses the request.",
  },
];

export default function AboutPage() {
  return (
    <div>
      <Sidebar activePage="about" />

      <main className="flex-1 ml-64 bg-background relative min-h-screen">
        <TopBar accent="primary" statusText="About" statusLabel="About_Gremlyn" />

        <div className="p-8 max-w-5xl mx-auto space-y-12 pb-24">
          {/* Hero */}
          <div className="border-b border-outline-variant/20 pb-8">
            <span className="font-mono text-primary text-xs font-bold tracking-[0.2em] block mb-2">
              ABOUT
            </span>
            <h3 className="text-5xl font-headline font-black tracking-tight text-on-surface">
              GREMLYN
            </h3>
            <p className="text-on-surface-variant font-mono text-sm mt-3 max-w-2xl leading-relaxed">
              Security and resilience platform for AI agents. We break your agents
              before anyone else does.
            </p>
            <div className="flex gap-4 mt-6">
              <span className="font-mono text-[10px] px-3 py-1.5 bg-primary/10 text-primary border border-primary/20 uppercase tracking-widest font-bold">
                beta
              </span>
              <span className="font-mono text-[10px] px-3 py-1.5 bg-surface-container-high text-on-surface-variant border border-outline-variant/20 uppercase tracking-widest">
                OPEN SOURCE
              </span>
            </div>
          </div>

          {/* Vision */}
          <div className="bg-surface-container-low p-8 border-l-2 border-primary">
            <h4 className="font-headline text-sm font-bold uppercase tracking-widest text-primary mb-3">
              MISSION
            </h4>
            <p className="font-mono text-xs text-on-surface-variant leading-relaxed">
              AI agents are gaining access to critical tools: databases, APIs, payment systems, internal
              infrastructure. But there&apos;s no firewall for these connections. No chaos testing to verify
              resilience. Gremlyn fills this gap with two products:{" "}
              <span className="text-primary font-bold">Shield</span> (production defense) and{" "}
              <span className="text-secondary font-bold">Arena</span> (pre-production attack testing).
            </p>
          </div>

          {/* Products */}
          <div className="space-y-6">
            {FEATURES.map((feature) => (
              <div key={feature.title} className="bg-surface-container-low p-8 space-y-4">
                <div className="flex items-center gap-3">
                  <span
                    className={`material-symbols-outlined ${
                      feature.accent === "primary" ? "text-primary" : "text-secondary"
                    }`}
                    style={feature.icon === "shield" ? { fontVariationSettings: "'FILL' 1" } : undefined}
                  >
                    {feature.icon}
                  </span>
                  <h4
                    className={`font-headline text-lg font-bold uppercase tracking-widest ${
                      feature.accent === "primary" ? "text-primary" : "text-secondary"
                    }`}
                  >
                    {feature.title}
                  </h4>
                </div>
                <p className="font-mono text-xs text-on-surface-variant leading-relaxed">
                  {feature.description}
                </p>
                <div className="space-y-2 pt-2">
                  {feature.capabilities.map((cap) => (
                    <div key={cap} className="flex items-start gap-3">
                      <span
                        className={`text-[10px] mt-0.5 ${
                          feature.accent === "primary" ? "text-primary" : "text-secondary"
                        }`}
                      >
                        &gt;
                      </span>
                      <span className="font-mono text-[11px] text-on-surface-variant">{cap}</span>
                    </div>
                  ))}
                </div>
              </div>
            ))}
          </div>

          {/* Use Cases */}
          <div className="space-y-4">
            <h4 className="font-headline text-sm font-bold uppercase tracking-widest text-primary">
              PRODUCTION_USE_CASES
            </h4>
            {USE_CASES.map((uc) => (
              <div key={uc.title} className="bg-surface-container-low p-6 space-y-3">
                <h5 className="font-headline text-xs font-bold uppercase tracking-widest text-on-surface">
                  {uc.title}
                </h5>
                <p className="font-mono text-[11px] text-on-surface-variant leading-relaxed">
                  {uc.description}
                </p>
                <div className="bg-surface-container-lowest p-3 border-l-2 border-primary/30">
                  <span className="font-mono text-[10px] text-primary/70">{uc.example}</span>
                </div>
              </div>
            ))}
          </div>

          {/* Architecture */}
          <div className="bg-surface-container-low p-8">
            <h4 className="font-headline text-sm font-bold uppercase tracking-widest text-primary mb-4">
              ARCHITECTURE
            </h4>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              {[
                { label: "SHIELD_API", port: ":8081", tech: "Go + chi" },
                { label: "ARENA_API", port: ":8082", tech: "Go + chi + WS" },
                { label: "DASHBOARD", port: ":3000", tech: "Next.js 15" },
                { label: "CLI", port: "binary", tech: "Go + Cobra" },
              ].map((svc) => (
                <div key={svc.label} className="bg-surface-container-lowest p-4">
                  <span className="font-mono text-[10px] text-on-surface-variant block mb-1">
                    {svc.label}
                  </span>
                  <span className="font-mono text-xs text-primary font-bold block">{svc.port}</span>
                  <span className="font-mono text-[10px] text-on-surface-variant">{svc.tech}</span>
                </div>
              ))}
            </div>
          </div>

          {/* Footer tagline */}
          <div className="text-center py-8 border-t border-outline-variant/20">
            <p className="font-headline text-lg font-bold text-on-surface-variant uppercase tracking-widest">
              &quot;STRESS LESS, STRESS-TEST, PROTECT YOUR AI AGENT.&quot;
            </p>
          </div>
        </div>
      </main>
    </div>
  );
}
