import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { apiFetch, ApiError } from "@/api/client";

describe("apiFetch", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("returns data and etag on success", async () => {
    const mockData = { trees: [] };
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: {
        get: (h: string) => (h === "etag" ? '"abc123"' : null),
      },
      json: () => Promise.resolve(mockData),
    });
    vi.stubGlobal("fetch", mockFetch);

    const result = await apiFetch<typeof mockData>("/v1/trees");
    expect(result.data).toEqual(mockData);
    expect(result.etag).toBe('"abc123"');
  });

  it("throws ApiError with RFC 7807 body on non-ok response", async () => {
    const problem = {
      type: "about:blank",
      title: "Not Found",
      status: 404,
      detail: "tree not found",
    };
    const mockFetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
      statusText: "Not Found",
      headers: {
        get: (h: string) =>
          h === "content-type" ? "application/problem+json" : null,
      },
      json: () => Promise.resolve(problem),
    });
    vi.stubGlobal("fetch", mockFetch);

    await expect(apiFetch("/v1/trees/missing")).rejects.toThrow(ApiError);
    await expect(apiFetch("/v1/trees/missing")).rejects.toMatchObject({
      status: 404,
      problem: { detail: "tree not found" },
    });
  });
});
