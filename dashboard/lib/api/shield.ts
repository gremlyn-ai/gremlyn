import { shieldApi } from "./client";
import type {
  ShieldEvent,
  Rule,
  Alert,
  ServerInfo,
  ShieldStatusResponse,
  ShieldMetricsResponse,
  ShieldEventListResponse,
  RuleListResponse,
  AlertListResponse,
  ServerListResponse,
} from "./types";

export async function getStatus(): Promise<ShieldStatusResponse> {
  return shieldApi<ShieldStatusResponse>("/api/v1/status");
}

export async function getMetrics(): Promise<ShieldMetricsResponse> {
  return shieldApi<ShieldMetricsResponse>("/api/v1/metrics");
}

export async function getEvents(serverId?: string): Promise<ShieldEvent[]> {
  const path = serverId ? `/api/v1/events?server_id=${serverId}` : "/api/v1/events";
  const res = await shieldApi<ShieldEventListResponse>(path);
  return res.events;
}

export async function getBlockedEvents(limit?: number): Promise<ShieldEvent[]> {
  const path = limit ? `/api/v1/events/blocked?limit=${limit}` : "/api/v1/events/blocked";
  const res = await shieldApi<ShieldEventListResponse>(path);
  return res.events;
}

export async function getRules(serverId?: string): Promise<Rule[]> {
  const path = serverId ? `/api/v1/rules?server_id=${serverId}` : "/api/v1/rules";
  const res = await shieldApi<RuleListResponse>(path);
  return res.rules;
}

export async function createRule(rule: Partial<Rule>): Promise<Rule> {
  return shieldApi<Rule>("/api/v1/rules", {
    method: "POST",
    body: JSON.stringify(rule),
  });
}

export async function toggleRule(ruleId: string, enabled: boolean): Promise<Rule> {
  return shieldApi<Rule>(`/api/v1/rules/${ruleId}`, {
    method: "PUT",
    body: JSON.stringify({ enabled }),
  });
}

export async function getAlerts(): Promise<Alert[]> {
  const res = await shieldApi<AlertListResponse>("/api/v1/alerts");
  return res.alerts;
}

// ── Servers ──

export async function listServers(): Promise<ServerInfo[]> {
  const res = await shieldApi<ServerListResponse>("/api/v1/servers");
  return res.servers;
}

export async function createServer(server: {
  name: string;
  mode: string;
  upstream_url?: string;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  auth_header?: string;
  headers?: Record<string, string>;
}): Promise<ServerInfo> {
  return shieldApi<ServerInfo>("/api/v1/servers", {
    method: "POST",
    body: JSON.stringify(server),
  });
}

export async function updateServer(
  id: string,
  updates: Partial<ServerInfo>
): Promise<ServerInfo> {
  return shieldApi<ServerInfo>(`/api/v1/servers/${id}`, {
    method: "PUT",
    body: JSON.stringify(updates),
  });
}

export async function deleteServer(id: string): Promise<void> {
  await shieldApi<{ deleted: string }>(`/api/v1/servers/${id}`, {
    method: "DELETE",
  });
}
