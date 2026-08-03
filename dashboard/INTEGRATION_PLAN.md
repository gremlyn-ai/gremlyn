# Frontend ↔ Backend Integration Plan

## Executive Summary

Wire the gremlyn-dashboard (Next.js 15) to the Shield (:8081) and Arena (:8082) Go APIs. Every component currently renders hardcoded mock data. The API client layer, Zustand stores, and WebSocket class already exist but are disconnected. This plan bridges them.

---

## Architecture Decision: SWR + Zustand

**Constraint from CLAUDE.md:** "NO useEffect for data fetching. Use Next.js server components or SWR."

**Decision:** Install SWR. Use custom SWR hooks that fetch via the existing API client functions and sync results into Zustand stores. This gives us:

- Automatic revalidation, retry, and caching (SWR)
- Global state sharing across components (Zustand)
- No useEffect anywhere
- Graceful degradation when backends are offline (loading/error states)
- Components stay pure — they read from hooks/stores, never call APIs directly

**Why not server components:** The APIs are on separate ports (microservices). Server components would need the backend running at build/SSR time. With SWR, pages load instantly and populate when the API responds.

---

## Phase 0: Foundation (do first)

### 0.1 Install SWR

```bash
npm install swr
```

### 0.2 Create `.env.local`

**File:** `.env.local` (new)

```env
NEXT_PUBLIC_SHIELD_URL=http://localhost:8081
NEXT_PUBLIC_ARENA_URL=http://localhost:8082
NEXT_PUBLIC_ARENA_WS_URL=ws://localhost:8082
```

No backend changes needed — CORS is already configured for `localhost:3000`.

### 0.3 Add missing types to `lib/api/types.ts`

The backend's Shield `/metrics` and `/status` endpoints have no corresponding frontend types.

```typescript
// Add to types.ts:

export type ShieldStatusResponse = {
  running: boolean;
  servers: string[];
  uptime?: string;
};

export type ShieldMetricsResponse = {
  event_counts: Record<string, number>;
  alert_counts: Record<string, number>;
  period_hours: number;
};
```

### 0.4 Add missing API functions to `lib/api/shield.ts`

```typescript
// Add:
export async function getStatus(): Promise<ShieldStatusResponse> {
  return shieldApi<ShieldStatusResponse>("/api/v1/status");
}

export async function getMetrics(): Promise<ShieldMetricsResponse> {
  return shieldApi<ShieldMetricsResponse>("/api/v1/metrics");
}

export async function getBlockedEvents(limit?: number): Promise<ShieldEvent[]> {
  const path = limit ? `/api/v1/events/blocked?limit=${limit}` : "/api/v1/events/blocked";
  const res = await shieldApi<EventListResponse>(path);
  return res.events;
}
```

### 0.5 Expand Zustand stores

**`lib/stores/shieldStore.ts`** — add metrics + status + loading states:

```typescript
type ShieldState = {
  // Data
  recentEvents: ShieldEvent[];
  rules: Rule[];
  alerts: Alert[];
  metrics: ShieldMetricsResponse | null;
  status: ShieldStatusResponse | null;

  // Loading
  loading: boolean;
  error: string | null;

  // Actions
  setEvents: (events: ShieldEvent[]) => void;
  setRules: (rules: Rule[]) => void;
  setAlerts: (alerts: Alert[]) => void;
  setMetrics: (m: ShieldMetricsResponse) => void;
  setStatus: (s: ShieldStatusResponse) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  toggleRule: (ruleId: string) => void;
};
```

**`lib/stores/arenaStore.ts`** — add gremlins list + loading:

```typescript
type ArenaState = {
  currentSession: ArenaSession | null;
  events: ArenaEvent[];
  gremlins: GremlinInfo[];
  wsConnected: boolean;
  loading: boolean;
  error: string | null;

  setSession: (session: ArenaSession | null) => void;
  addEvent: (event: ArenaEvent) => void;
  clearEvents: () => void;
  setGremlins: (g: GremlinInfo[]) => void;
  setWsConnected: (connected: boolean) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
};
```

---

## Phase 1: SWR Hooks Layer (new file)

### 1.1 Create `lib/hooks/useShieldData.ts`

This is the bridge between API → SWR → Zustand. Each hook fetches data with SWR and pushes it into the store.

```typescript
import useSWR from "swr";
import { getEvents, getRules, getAlerts, getMetrics, getStatus, getBlockedEvents } from "@/lib/api/shield";
import { useShieldStore } from "@/lib/stores/shieldStore";

const SWR_OPTIONS = { refreshInterval: 30_000, revalidateOnFocus: true };

export function useShieldStatus() {
  const setStatus = useShieldStore((s) => s.setStatus);
  return useSWR("shield-status", getStatus, {
    ...SWR_OPTIONS,
    onSuccess: (data) => setStatus(data),
  });
}

export function useShieldMetrics() {
  const setMetrics = useShieldStore((s) => s.setMetrics);
  return useSWR("shield-metrics", getMetrics, {
    ...SWR_OPTIONS,
    onSuccess: (data) => setMetrics(data),
  });
}

export function useShieldEvents() {
  const setEvents = useShieldStore((s) => s.setEvents);
  return useSWR("shield-events", () => getEvents(), {
    ...SWR_OPTIONS,
    onSuccess: (data) => setEvents(data),
  });
}

export function useBlockedEvents(limit = 1) {
  return useSWR(
    `shield-blocked-${limit}`,
    () => getBlockedEvents(limit),
    SWR_OPTIONS
  );
}

export function useShieldRules() {
  const setRules = useShieldStore((s) => s.setRules);
  return useSWR("shield-rules", () => getRules(), {
    ...SWR_OPTIONS,
    onSuccess: (data) => setRules(data),
  });
}

export function useShieldAlerts() {
  const setAlerts = useShieldStore((s) => s.setAlerts);
  return useSWR("shield-alerts", getAlerts, {
    ...SWR_OPTIONS,
    onSuccess: (data) => setAlerts(data),
  });
}
```

### 1.2 Create `lib/hooks/useArenaData.ts`

```typescript
import useSWR from "swr";
import { listGremlins, getStatus, getSession, getSessionEvents } from "@/lib/api/arena";
import { useArenaStore } from "@/lib/stores/arenaStore";

export function useArenaGremlins() {
  const setGremlins = useArenaStore((s) => s.setGremlins);
  return useSWR("arena-gremlins", listGremlins, {
    revalidateOnFocus: false,
    onSuccess: (data) => setGremlins(data),
  });
}

export function useArenaStatus() {
  return useSWR("arena-status", getStatus, { refreshInterval: 10_000 });
}

export function useArenaSession(id: string | null) {
  const setSession = useArenaStore((s) => s.setSession);
  return useSWR(
    id ? `arena-session-${id}` : null,
    () => getSession(id!),
    {
      refreshInterval: 5_000,
      onSuccess: (data) => setSession(data),
    }
  );
}

export function useSessionEvents(id: string | null) {
  return useSWR(
    id ? `arena-events-${id}` : null,
    () => getSessionEvents(id!),
    { refreshInterval: 5_000 }
  );
}
```

### 1.3 Create `lib/hooks/useArenaWebSocket.ts`

Custom hook that manages the WebSocket lifecycle without useEffect — uses SWR's subscription pattern or useRef-based initialization:

```typescript
import { useRef, useCallback } from "react";
import { useSyncExternalStore } from "react";
import { ArenaWebSocket } from "@/lib/websocket";
import { useArenaStore } from "@/lib/stores/arenaStore";
import type { ArenaEvent } from "@/lib/api/types";

export function useArenaWebSocket(sessionId: string | null) {
  const wsRef = useRef<ArenaWebSocket | null>(null);
  const addEvent = useArenaStore((s) => s.addEvent);
  const setWsConnected = useArenaStore((s) => s.setWsConnected);

  const connect = useCallback(() => {
    if (!sessionId) return;
    wsRef.current?.disconnect();

    const ws = new ArenaWebSocket(sessionId);
    ws.subscribe((event: ArenaEvent) => addEvent(event));
    ws.connect();
    wsRef.current = ws;

    // Poll connected state
    const interval = setInterval(() => {
      setWsConnected(ws.connected);
    }, 1000);

    return () => {
      clearInterval(interval);
      ws.disconnect();
      setWsConnected(false);
    };
  }, [sessionId, addEvent, setWsConnected]);

  const disconnect = useCallback(() => {
    wsRef.current?.disconnect();
    wsRef.current = null;
    setWsConnected(false);
  }, [setWsConnected]);

  return { connect, disconnect, wsRef };
}
```

**Note:** The Arena page will call `connect()` after `createSession()` succeeds — this is an event handler, not a useEffect.

---

## Phase 2: Shield Components

### 2.1 `MetricCards.tsx` — Wire to `/status` + `/metrics`

**Current:** Hardcoded "12,482 blocked", "842 agents", "56 servers", "99.98% uptime".

**Change to:** Accept props, show loading skeleton when data is null.

**Data mapping:**

| Card | Source | Field |
|------|--------|-------|
| Blocked (24h) | `GET /metrics` | `event_counts["blocked"]` (sum blocked actions) |
| Agents Protected | `GET /status` | Not available — keep placeholder or derive from events |
| Servers | `GET /status` | `servers.length` |
| Uptime | `GET /status` | `uptime` string |

**New signature:**

```typescript
type MetricCardsProps = {
  blockedCount: number | null;
  serverCount: number | null;
  uptime: string | null;
  alertCounts: Record<string, number> | null;
};

export function MetricCards({ blockedCount, serverCount, uptime, alertCounts }: MetricCardsProps) {
  // Build metrics array from props, show "---" placeholders when null
}
```

**Loading state:** When a value is `null`, render a pulsing `bg-surface-container` block in place of the number (skeleton).

### 2.2 `ThreatCard.tsx` — Wire to latest blocked event

**Current:** Hardcoded fake UUID, SQL injection details, static log lines.

**Change to:** Accept the most recent blocked `ShieldEvent` as a prop.

```typescript
type ThreatCardProps = {
  event: ShieldEvent | null;
};
```

**Data mapping:**

| Field | Source |
|-------|--------|
| UUID | `event.id` (truncate to last 16 chars) |
| Attack Type | `event.detail` |
| Source IP | Parse from `event.raw_message` or show "CLASSIFIED" |
| Detection Vector | `event.method` |
| Log lines | Build from `event.raw_message`, `event.action`, `event.created_at` |
| CRITICAL_EVENT badge | Show when `event.action === "block"` |

**Empty state:** When `event` is null, show "NO_RECENT_THREATS" with a green shield icon.

### 2.3 `ThreatChart.tsx` — Wire to events aggregation

**Current:** 7 hardcoded bars with fixed heights.

**Options (ordered by preference):**

1. **Client-side aggregation**: Fetch last 7 days of events via `getEvents()`, group by day, count. Simple but requires enough events in the DB.
2. **Use `/metrics` endpoint**: The backend returns `event_counts` by action type, but not by day. This gives totals, not a time series.
3. **Keep mock for now**: If neither option works well, keep the visual mock and add a `// TODO: needs time-series endpoint` comment.

**Recommendation:** Option 1 — fetch events and aggregate client-side. Accept events as props:

```typescript
type ThreatChartProps = {
  events: ShieldEvent[];
};
```

Build the 7-day bars by grouping `event.created_at` by day and counting.

### 2.4 `RuleTable.tsx` — Wire to rules API + toggle persistence

**Current:** 4 hardcoded rules, local useState toggle (not persisted).

**Changes:**

1. Accept `rules: Rule[]` as prop (from SWR hook)
2. Toggle calls `toggleRule(ruleId, !enabled)` from `lib/api/shield.ts` (PUT request)
3. Optimistic update: flip in Zustand store immediately, revert on API error
4. Map Rule fields to table columns:

| Column | Rule field |
|--------|-----------|
| Policy Name | `rule.name` |
| Trigger | `rule.detect` (string or joined string[]) + `rule.match?.tool` if present |
| Action | `rule.action` (style "error" for block/block_and_alert, "primary" for others) |
| Hits (24h) | Not available from API — hide column, show `rule.scan_responses ? "IN+OUT" : "IN"` instead |
| Enforcement | `rule.enabled` toggle |

**New signature:**

```typescript
type RuleTableProps = {
  rules: Rule[];
  onToggleRule: (ruleId: string, enabled: boolean) => void;
};
```

### 2.5 `app/shield/page.tsx` — Orchestration

**Current:** Pure composition, no data fetching.

**Change to:** `"use client"` directive. Call SWR hooks, pass data as props to children.

```typescript
"use client";

export default function ShieldDashboard() {
  const { data: status } = useShieldStatus();
  const { data: metrics } = useShieldMetrics();
  const { data: blockedEvents } = useBlockedEvents(1);
  const { data: rules, mutate: mutateRules } = useShieldRules();
  const events = useShieldStore((s) => s.recentEvents);

  const latestBlocked = blockedEvents?.[0] ?? null;

  async function handleToggleRule(ruleId: string, enabled: boolean) {
    // Optimistic update in store
    useShieldStore.getState().toggleRule(ruleId);
    try {
      await toggleRule(ruleId, enabled);
      mutateRules(); // revalidate
    } catch {
      useShieldStore.getState().toggleRule(ruleId); // revert
    }
  }

  return (
    <>
      <MetricCards
        blockedCount={metrics?.event_counts?.blocked ?? null}
        serverCount={status?.servers?.length ?? null}
        uptime={status?.uptime ?? null}
        alertCounts={metrics?.alert_counts ?? null}
      />
      <ThreatCard event={latestBlocked} />
      <ThreatChart events={events} />
      <RuleTable
        rules={rules ?? []}
        onToggleRule={handleToggleRule}
      />
    </>
  );
}
```

---

## Phase 3: Arena Components

### 3.1 `GremlinSelector.tsx` — Wire to `/arena/gremlins`

**Current:** 4 hardcoded gremlins with local toggle state.

**Changes:**

1. Accept `gremlins: GremlinInfo[]` from the API (fetched by parent)
2. Keep local toggle state for selection (this IS local UI state — which gremlins to include in a session)
3. Expose selected gremlins via callback prop

```typescript
type GremlinSelectorProps = {
  gremlins: GremlinInfo[];
  selected: string[];
  onSelectionChange: (selected: string[]) => void;
};
```

**Backend gremlins (4 registered):** hallucination, latency, corruption, loop

**Display mapping:** `gremlin.name` → uppercase with underscores (e.g., `"hallucination"` → `"HALLUCINATION"`). Use `gremlin.description` directly.

**Loading state:** Show 4 skeleton cards when `gremlins` is empty.

### 3.2 `ArenaControls.tsx` — Wire target agent + lift state

**Current:** 3 hardcoded agent options, local intensity state.

**Problem:** There's no backend endpoint for listing available servers/agents. The Shield API has `GET /status` → `servers[]`, but Arena doesn't know about Shield's servers.

**Options:**

1. **Cross-service call**: Arena page fetches Shield's `/status` to get server list
2. **Hardcode for now**: Keep the dropdown static until a proper endpoint exists
3. **Config-driven**: Read from gremlyn.yaml (not accessible from frontend)

**Recommendation:** Option 1 — fetch Shield status for the server list. It's one extra SWR call and solves the problem cleanly.

**Changes:**

1. Accept props for server list and state callbacks
2. Lift intensity and target agent state to parent

```typescript
type ArenaControlsProps = {
  servers: string[];
  selectedServer: string;
  onServerChange: (server: string) => void;
  intensity: number;
  onIntensityChange: (n: number) => void;
};
```

### 3.3 `app/arena/page.tsx` — Full orchestration

**Current:** Composition with hardcoded "ARENA-992-X" session ID.

**Change to:** `"use client"`. Manages session creation flow:

```
[Select Gremlins] → [Set Target + Intensity] → [LAUNCH CHAOS] → [createSession()] → [WebSocket connect] → [Live events]
```

**State management:**

```typescript
"use client";

export default function ArenaPage() {
  // API data
  const { data: gremlins } = useArenaGremlins();
  const { data: shieldStatus } = useShieldStatus(); // for server list

  // Local form state
  const [selectedGremlins, setSelectedGremlins] = useState<string[]>([]);
  const [targetServer, setTargetServer] = useState("");
  const [intensity, setIntensity] = useState(7);
  const [prompt, setPrompt] = useState("");

  // Session state from store
  const session = useArenaStore((s) => s.currentSession);
  const events = useArenaStore((s) => s.events);
  const wsConnected = useArenaStore((s) => s.wsConnected);
  const { connect, disconnect } = useArenaWebSocket(session?.id ?? null);

  async function handleLaunch() {
    const newSession = await createSession(
      targetServer,
      selectedGremlins,
      String(intensity),
      prompt ? [prompt] : []
    );
    useArenaStore.getState().setSession(newSession);
    useArenaStore.getState().clearEvents();
    connect();
  }

  // Set default server when list loads
  // (handled via SWR onSuccess, not useEffect)

  return (/* ... */);
}
```

**Post-launch UI change:** After `handleLaunch()`, the page should transition to show live session status — either inline (replace the launch button with a progress indicator) or navigate to a session detail page.

---

## Phase 4: Shared Components

### 4.1 `TopBar.tsx` — Wire status indicators

**Current:** Hardcoded "Vitals: Stable", "Nodes: 1,024".

**Change:** Accept optional status data as props. The Shield page passes Shield status, Arena page passes Arena status.

```typescript
type TopBarProps = {
  accent: "primary" | "secondary";
  statusText: string;
  statusLabel: string;
  nodeCount?: number;
  agentCount?: number;
  isConnected?: boolean;
};
```

### 4.2 `StatusFooter.tsx` — Wire system status

**Current:** Hardcoded "LATENCY: 12ms", "THREAD_POOL: 2,492/4,000".

**Option:** Keep as-is for now (cosmetic chrome). These would need a dedicated backend metrics endpoint. Mark as `// TODO: wire to backend health endpoint`.

### 4.3 Create a shared loading skeleton component

**File:** `components/Skeleton.tsx`

```typescript
export function Skeleton({ className }: { className?: string }) {
  return (
    <div className={`animate-pulse bg-surface-container-high ${className ?? "h-4 w-24"}`} />
  );
}
```

### 4.4 Create a shared error banner component

**File:** `components/ErrorBanner.tsx`

```typescript
export function ErrorBanner({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="bg-error/10 border border-error/20 p-4 flex items-center justify-between">
      <span className="font-mono text-xs text-error">[ERROR] {message}</span>
      {onRetry && (
        <button onClick={onRetry} className="font-mono text-xs text-primary hover:underline">
          RETRY
        </button>
      )}
    </div>
  );
}
```

---

## Phase 5: Error Handling Strategy

### Connection failures (backend offline)

SWR handles this automatically. When fetch fails:
- `data` stays `undefined` (or previous cached value)
- `error` is set
- SWR retries with exponential backoff

**Each page shows an ErrorBanner at the top when the API is unreachable:**

```typescript
const { error: statusError } = useShieldStatus();
// ...
{statusError && <ErrorBanner message="Shield API unreachable" onRetry={() => mutate()} />}
```

### Optimistic updates (rule toggle)

1. Immediately update Zustand store
2. Fire API call
3. On success: SWR revalidates (confirms)
4. On failure: Revert Zustand store + show toast/banner

### WebSocket disconnection

The ArenaWebSocket class already handles auto-reconnect (3s delay). The `wsConnected` state in the store drives a UI indicator (green/red dot in TopBar).

---

## Implementation Order

```
Phase 0 (foundation)     ~30 min
├── 0.1 npm install swr
├── 0.2 .env.local
├── 0.3 types.ts additions
├── 0.4 shield.ts additions
└── 0.5 store expansions

Phase 1 (hooks)           ~45 min
├── 1.1 useShieldData.ts
├── 1.2 useArenaData.ts
└── 1.3 useArenaWebSocket.ts

Phase 2 (Shield wiring)   ~1.5 hr
├── 2.1 MetricCards (props + skeleton)
├── 2.2 ThreatCard (props + empty state)
├── 2.3 ThreatChart (props + aggregation)
├── 2.4 RuleTable (props + toggle API)
└── 2.5 Shield page orchestration

Phase 3 (Arena wiring)    ~1.5 hr
├── 3.1 GremlinSelector (API gremlins)
├── 3.2 ArenaControls (server list + lifted state)
└── 3.3 Arena page orchestration + launch flow

Phase 4 (shared)          ~30 min
├── 4.1 TopBar status props
├── 4.2 Skeleton component
└── 4.3 ErrorBanner component

Phase 5 (testing)         ~30 min
├── Start both backends
├── Verify Shield page loads with real data
├── Verify rule toggle persists
├── Create Arena session, verify WebSocket events
└── Kill backends, verify graceful degradation
```

---

## Files Changed (Summary)

| File | Action | What |
|------|--------|------|
| `package.json` | modify | add `swr` |
| `.env.local` | create | API URLs |
| `lib/api/types.ts` | modify | add `ShieldStatusResponse`, `ShieldMetricsResponse` |
| `lib/api/shield.ts` | modify | add `getStatus()`, `getMetrics()`, `getBlockedEvents()` |
| `lib/stores/shieldStore.ts` | modify | add metrics, status, loading, error fields |
| `lib/stores/arenaStore.ts` | modify | add gremlins, loading, error fields |
| `lib/hooks/useShieldData.ts` | create | SWR hooks for Shield |
| `lib/hooks/useArenaData.ts` | create | SWR hooks for Arena |
| `lib/hooks/useArenaWebSocket.ts` | create | WebSocket lifecycle hook |
| `app/shield/page.tsx` | modify | add "use client", SWR hooks, pass props |
| `app/shield/components/MetricCards.tsx` | modify | accept props, add skeleton |
| `app/shield/components/ThreatCard.tsx` | modify | accept ShieldEvent prop |
| `app/shield/components/ThreatChart.tsx` | modify | accept events[], aggregate |
| `app/shield/components/RuleTable.tsx` | modify | accept Rule[], toggle callback |
| `app/arena/page.tsx` | modify | add "use client", session flow, WS |
| `app/arena/components/GremlinSelector.tsx` | modify | accept GremlinInfo[] |
| `app/arena/components/ArenaControls.tsx` | modify | accept servers[], lift state |
| `components/TopBar.tsx` | modify | accept status props |
| `components/Skeleton.tsx` | create | loading placeholder |
| `components/ErrorBanner.tsx` | create | error display |

**No backend changes required** — CORS is already configured, all endpoints exist.

---

## CRITICAL: Type Mismatches (Phase 0 blocker)

The frontend `types.ts` was written speculatively and **diverges significantly** from the actual Go models in `gremlyn-core/pkg/models/models.go`. These must be fixed before any wiring.

### Shield Event — frontend vs backend

| Frontend `ShieldEvent` | Backend `models.Event` | Status |
|------------------------|----------------------|--------|
| `id` | `id` | OK |
| `server_id` | `server_id` | OK |
| `rule_id` | — (doesn't exist) | REMOVE |
| `direction` | `direction` | OK |
| `action` (allow/block/redact/modify) | `action_taken` (allowed/blocked/redacted/alerted) | RENAME + fix values |
| `method` | `message_type` | RENAME |
| `detail` | — | REMOVE |
| `raw_message` | — | REMOVE |
| `created_at` | `created_at` | OK |
| — | `session_id` | ADD |
| — | `tool_name` | ADD |
| — | `tool_args` (json) | ADD |
| — | `response_payload` (json) | ADD |
| — | `rules_triggered` (string[]) | ADD |
| — | `detection_results` (map) | ADD |
| — | `latency_ms` | ADD |
| — | `timestamp` | ADD |

### Shield Rule — frontend vs backend

The frontend `Rule` type is **completely wrong**. Backend has no `priority`, `direction`, `conditions[]`, or `action_config`.

**Correct Rule type (from Go):**

```typescript
export type Rule = {
  id: string;
  server_id?: string;
  name: string;
  match?: RuleMatch;
  scan_responses?: boolean;
  scan_outgoing?: boolean;
  detect?: string | string[];  // DetectConfig serializes as string or string[]
  entity_field?: string;
  action: RuleAction;
  enabled: boolean;
  source?: string;
  original_text?: string;
  config?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
};

export type RuleMatch = {
  tool?: string;
  args?: Record<string, RuleMatchCondition>;
  time?: { outside?: string; timezone?: string };
};

export type RuleMatchCondition = {
  greater_than?: number;
  less_than?: number;
  equals?: unknown;
  must_start_with?: string;
  not_contains?: string[];
  contains?: string[];
  regex?: string;
};

export type RuleAction = "allow" | "block" | "block_and_alert" | "redact"
  | "redact_and_alert" | "throttle" | "pause_and_request_approval" | "log_only";
```

### Shield Alert — frontend vs backend

| Frontend | Backend | Fix |
|----------|---------|-----|
| `status`: "pending"\|"acknowledged"\|"resolved" | `status`: "new"\|"acknowledged"\|"resolved"\|"false_positive" | Fix enum |
| — | `type` (string) | ADD |
| — | `notified_channels` (string[]) | ADD |
| — | `resolved_at` (string\|null) | ADD |
| `rule_id` | — (doesn't exist) | REMOVE |

### Arena types — mostly correct

Arena types are close. Main fixes:

- `ArenaSession.config` should be `Record<string, unknown>` (json.RawMessage), not typed `SessionConfig`
- `ArenaSession.results` should be `Record<string, unknown> | null` (json.RawMessage), not typed `ResilienceReport`
- `ArenaEvent.gremlin_config` should be `Record<string, unknown>`
- `ArenaEvent.agent_response` and `details` should be `Record<string, unknown> | null`

**Alternative approach:** Keep the typed `SessionConfig` and `ResilienceReport` on the frontend for better DX, but cast from the raw JSON response. The backend sends these as `json.RawMessage` but the actual shape IS those types.

### Action: Rewrite `types.ts` in Phase 0

Before any component wiring, rewrite `lib/api/types.ts` to match the actual Go models. This is ~30 min of work but prevents cascading bugs.

---

## Open Questions

1. **Agents Protected metric**: Shield API doesn't expose agent count. Options: (a) derive from unique `server_id` values in events, (b) add endpoint to Shield, (c) drop the card. Recommendation: (a) for now.

2. **Hits (24h) column in RuleTable**: Backend doesn't track per-rule hit counts. Options: (a) count from events client-side by matching `rule_id`, (b) add to backend, (c) hide column. Recommendation: (c) hide for now, show rule `description` instead.

3. **Arena session detail page**: After launching chaos, the user needs to see live results. Current plan shows inline on the arena page. A dedicated `/arena/session/[id]` route (referenced in claude.md) would be better but is out of scope for this integration pass.

4. **Default server selection**: When Arena page loads the server list from Shield, which one is pre-selected? Recommendation: first in the list, or empty with a "SELECT_TARGET" placeholder.
