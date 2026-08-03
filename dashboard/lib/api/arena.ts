import { arenaApi } from "./client";
import type {
  ArenaSession,
  ArenaEvent,
  GremlinInfo,
  ArenaStatusResponse,
  SessionListResponse,
  ArenaEventListResponse,
  GremlinListResponse,
} from "./types";

export async function createSession(
  serverID: string,
  gremlins: string[],
  intensity: string,
  prompts?: string[]
): Promise<ArenaSession> {
  return arenaApi<ArenaSession>("/arena/sessions", {
    method: "POST",
    body: JSON.stringify({
      server_id: serverID,
      gremlins,
      intensity,
      prompts: prompts ?? [],
    }),
  });
}

export async function getSession(id: string): Promise<ArenaSession> {
  return arenaApi<ArenaSession>(`/arena/sessions/${id}`);
}

export async function listSessions(): Promise<ArenaSession[]> {
  const res = await arenaApi<SessionListResponse>("/arena/sessions");
  return res.sessions;
}

export async function stopSession(id: string): Promise<void> {
  await arenaApi<{ status: string }>(`/arena/sessions/${id}/stop`, {
    method: "POST",
  });
}

export async function getSessionEvents(id: string): Promise<ArenaEvent[]> {
  const res = await arenaApi<ArenaEventListResponse>(`/arena/sessions/${id}/events`);
  return res.events;
}

export async function listGremlins(): Promise<GremlinInfo[]> {
  const res = await arenaApi<GremlinListResponse>("/arena/gremlins");
  return res.gremlins;
}

export async function getStatus(): Promise<ArenaStatusResponse> {
  return arenaApi<ArenaStatusResponse>("/status");
}
