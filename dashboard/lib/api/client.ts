import type { ErrorResponse } from "./types";

const SHIELD_BASE_URL = process.env.NEXT_PUBLIC_SHIELD_URL ?? "http://localhost:8081";
const ARENA_BASE_URL = process.env.NEXT_PUBLIC_ARENA_URL ?? "http://localhost:8082";

export class ApiError extends Error {
  constructor(
    public status: number,
    public body: ErrorResponse
  ) {
    super(body.error);
    this.name = "ApiError";
  }
}

async function request<T>(baseUrl: string, path: string, init?: RequestInit): Promise<T> {
  const url = `${baseUrl}${path}`;
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...((init?.headers as Record<string, string>) ?? {}),
  };

  const res = await fetch(url, { ...init, headers });

  if (!res.ok) {
    const body = (await res.json().catch(() => ({ error: res.statusText }))) as ErrorResponse;
    throw new ApiError(res.status, body);
  }

  return res.json() as Promise<T>;
}

export function shieldApi<T>(path: string, init?: RequestInit): Promise<T> {
  return request<T>(SHIELD_BASE_URL, path, init);
}

export function arenaApi<T>(path: string, init?: RequestInit): Promise<T> {
  return request<T>(ARENA_BASE_URL, path, init);
}
