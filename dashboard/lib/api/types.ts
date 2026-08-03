/** Types matching Go backend structs exactly */

// ── Shared enums ──

export type Direction = "outgoing" | "incoming" | "both";

export type ActionTaken = "allowed" | "blocked" | "redacted" | "alerted";

export type RuleAction =
  | "allow"
  | "block"
  | "block_and_alert"
  | "redact"
  | "redact_and_alert"
  | "throttle"
  | "pause_and_request_approval"
  | "log_only";

export type AlertSeverity = "critical" | "high" | "medium" | "low";

export type AlertStatus = "new" | "acknowledged" | "resolved" | "false_positive";

export type SessionStatus = "running" | "completed" | "cancelled";

export type ArenaOutcome = "survived" | "crashed" | "degraded";

export type RuleSource = "yaml" | "api" | "natural_language";

// ── Shield types (from gremlyn-core/pkg/models) ──

export type ShieldEvent = {
  id: string;
  org_id?: string;
  server_id: string;
  session_id?: string;
  timestamp: string;
  direction: Direction;
  message_type: string;
  tool_name?: string;
  tool_args?: Record<string, unknown>;
  response_payload?: Record<string, unknown>;
  payload_ref?: string;
  action_taken: ActionTaken;
  rules_triggered?: string[];
  detection_results?: Record<string, string>;
  latency_ms: number;
  created_at?: string;
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

export type DetectConfig = string | string[];

export type Rule = {
  id: string;
  org_id?: string;
  server_id?: string;
  name: string;
  match?: RuleMatch;
  scan_responses?: boolean;
  scan_outgoing?: boolean;
  detect?: DetectConfig;
  entity_field?: string;
  action: RuleAction;
  enabled: boolean;
  source?: RuleSource;
  original_text?: string;
  config?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
};

export type Alert = {
  id: string;
  org_id?: string;
  event_id: string;
  severity: AlertSeverity;
  type: string;
  message: string;
  status: AlertStatus;
  notified_channels?: string[];
  created_at: string;
  resolved_at?: string | null;
};

// ── Server types ──

export type ServerInfo = {
  id: string;
  name: string;
  mode: "proxy" | "wrap" | "cloud";
  upstream_url?: string;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  auth_header?: string;
  headers?: Record<string, string>;
  status: "active" | "inactive" | "error";
  source: "api" | "yaml";
  created_at: string;
};

export type ServerListResponse = {
  servers: ServerInfo[];
};

// ── Arena types (from gremlyn-core/pkg/models) ──

export type ArenaSession = {
  id: string;
  org_id?: string;
  server_id: string;
  status: SessionStatus;
  config: Record<string, unknown>;
  results?: Record<string, unknown> | null;
  gremlins_sent: number;
  gremlins_survived: number;
  gremlins_crashed: number;
  started_at: string;
  completed_at?: string | null;
};

export type ArenaEvent = {
  id: string;
  session_id: string;
  gremlin_type: string;
  gremlin_config: Record<string, unknown>;
  injected_at: string;
  agent_response?: Record<string, unknown> | null;
  outcome: ArenaOutcome;
  score: number;
  details?: Record<string, unknown> | null;
};

export type GremlinInfo = {
  name: string;
  description: string;
};

// ── Typed overlays for raw JSON fields (client-side convenience) ──

export type SessionConfig = {
  gremlins: string[];
  intensity: string;
  prompts: string[];
};

export type ResilienceReport = {
  overall: number;
  grade: "excellent" | "good" | "needs_work" | "critical";
  dimensions: Record<string, DimensionScore>;
};

export type DimensionScore = {
  score: number;
  total: number;
  survived: number;
  degraded: number;
  crashed: number;
  weight: number;
  grade: string;
};

// ── Shield API response wrappers ──

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

export type ShieldEventListResponse = {
  events: ShieldEvent[];
  total: number;
};

export type RuleListResponse = {
  rules: Rule[];
};

export type AlertListResponse = {
  alerts: Alert[];
};

// ── Arena API response wrappers ──

export type SessionListResponse = {
  sessions: ArenaSession[];
};

export type ArenaEventListResponse = {
  events: ArenaEvent[];
};

export type GremlinListResponse = {
  gremlins: GremlinInfo[];
};

export type ArenaStatusResponse = {
  status: string;
  active_sessions: number;
  gremlins_available: number;
};

export type ErrorResponse = {
  error: string;
  code?: string;
};

// ── WebSocket message ──

export type WSMessage = {
  type: string;
  data: ArenaEvent | Record<string, unknown>;
};
