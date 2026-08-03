import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { shieldApi, arenaApi, ApiError } from "../client";

describe("API client", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    globalThis.fetch = vi.fn();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it("shieldApi calls correct base URL", async () => {
    const mockFetch = vi.mocked(globalThis.fetch);
    mockFetch.mockResolvedValueOnce(
      new Response(JSON.stringify({ rules: [] }), { status: 200 })
    );

    await shieldApi("/api/v1/rules");

    expect(mockFetch).toHaveBeenCalledWith(
      "http://localhost:8081/api/v1/rules",
      expect.objectContaining({
        headers: expect.objectContaining({
          "Content-Type": "application/json",
        }),
      })
    );
  });

  it("arenaApi calls correct base URL", async () => {
    const mockFetch = vi.mocked(globalThis.fetch);
    mockFetch.mockResolvedValueOnce(
      new Response(JSON.stringify({ sessions: [] }), { status: 200 })
    );

    await arenaApi("/api/v1/sessions");

    expect(mockFetch).toHaveBeenCalledWith(
      "http://localhost:8082/api/v1/sessions",
      expect.objectContaining({
        headers: expect.objectContaining({
          "Content-Type": "application/json",
        }),
      })
    );
  });

  it("throws ApiError on non-OK response", async () => {
    const mockFetch = vi.mocked(globalThis.fetch);
    mockFetch.mockResolvedValueOnce(
      new Response(JSON.stringify({ error: "Not found", code: "NOT_FOUND" }), {
        status: 404,
      })
    );

    await expect(shieldApi("/api/v1/rules/nonexistent")).rejects.toThrow(
      ApiError
    );

    try {
      mockFetch.mockResolvedValueOnce(
        new Response(
          JSON.stringify({ error: "Forbidden", code: "FORBIDDEN" }),
          { status: 403 }
        )
      );
      await shieldApi("/api/v1/admin");
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).status).toBe(403);
      expect((err as ApiError).message).toBe("Forbidden");
    }
  });

  it("includes Content-Type header", async () => {
    const mockFetch = vi.mocked(globalThis.fetch);
    mockFetch.mockResolvedValueOnce(
      new Response(JSON.stringify({}), { status: 200 })
    );

    await arenaApi("/api/v1/gremlins", {
      method: "POST",
      body: JSON.stringify({ name: "hallucination" }),
    });

    const callArgs = mockFetch.mock.calls[0];
    const headers = (callArgs[1] as RequestInit).headers as Record<string, string>;
    expect(headers["Content-Type"]).toBe("application/json");
  });

  it("handles non-JSON error response gracefully", async () => {
    const mockFetch = vi.mocked(globalThis.fetch);
    mockFetch.mockResolvedValueOnce(
      new Response("Internal Server Error", {
        status: 500,
        statusText: "Internal Server Error",
      })
    );

    await expect(shieldApi("/api/v1/status")).rejects.toThrow(ApiError);
  });

  it("passes custom headers alongside Content-Type", async () => {
    const mockFetch = vi.mocked(globalThis.fetch);
    mockFetch.mockResolvedValueOnce(
      new Response(JSON.stringify({ ok: true }), { status: 200 })
    );

    await shieldApi("/api/v1/protected", {
      headers: { Authorization: "Bearer test-token" },
    });

    const callArgs = mockFetch.mock.calls[0];
    const headers = (callArgs[1] as RequestInit).headers as Record<string, string>;
    expect(headers["Content-Type"]).toBe("application/json");
    expect(headers["Authorization"]).toBe("Bearer test-token");
  });
});
