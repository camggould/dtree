import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import React from "react";
import { useAggregateMetrics } from "@/api/query";
import type { Metrics, Tree } from "@/api/types.gen";

const mockTrees: Tree[] = [
  {
    slug: "alpha",
    schema_version: 1,
    name: "Alpha",
    archived: false,
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  },
  {
    slug: "beta",
    schema_version: 1,
    name: "Beta",
    archived: false,
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  },
];

const alphaMetrics: Metrics = {
  total_decisions: 5,
  by_status: { proposed: 2, decided: 2, out_of_scope: 1, superseded: 0 },
  by_priority: { assumption: 0, low: 1, medium: 2, high: 2, critical: 0 },
  by_creator: [
    { handle: "alice", count: 3 },
    { handle: "bob", count: 2 },
  ],
  assumptions_count: 0,
  unblocked_proposed_count: 2,
};

const betaMetrics: Metrics = {
  total_decisions: 8,
  by_status: { proposed: 3, decided: 4, out_of_scope: 0, superseded: 1 },
  by_priority: { assumption: 1, low: 2, medium: 2, high: 2, critical: 1 },
  by_creator: [
    { handle: "alice", count: 5 },
    { handle: "carol", count: 3 },
  ],
  assumptions_count: 1,
  unblocked_proposed_count: 3,
};

function makeWrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children);
}

describe("useAggregateMetrics", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) => {
        if (
          url.includes("/v1/trees") &&
          !url.includes("/metrics") &&
          !url.includes("/decisions")
        ) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: { get: () => null },
            json: () => Promise.resolve({ trees: mockTrees }),
          });
        }
        if (url.includes("/alpha/metrics")) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: { get: () => null },
            json: () => Promise.resolve(alphaMetrics),
          });
        }
        if (url.includes("/beta/metrics")) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: { get: () => null },
            json: () => Promise.resolve(betaMetrics),
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: () => null },
          json: () => Promise.resolve({}),
        });
      }),
    );
  });

  it("sums total_decisions and by_status across trees", async () => {
    const wrapper = makeWrapper();
    const { result } = renderHook(() => useAggregateMetrics(), { wrapper });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // Total: 5 + 8 = 13
    expect(result.current.total_decisions).toBe(13);

    // by_status sums
    expect(result.current.by_status["proposed"]).toBe(5); // 2 + 3
    expect(result.current.by_status["decided"]).toBe(6);  // 2 + 4
    expect(result.current.by_status["out_of_scope"]).toBe(1); // 1 + 0
    expect(result.current.by_status["superseded"]).toBe(1); // 0 + 1

    // by_creator: alice = 3+5=8, bob = 2, carol = 3 — sorted desc
    expect(result.current.by_creator[0]).toEqual({ handle: "alice", count: 8 });
    expect(result.current.by_creator[1]).toEqual({ handle: "carol", count: 3 });
    expect(result.current.by_creator[2]).toEqual({ handle: "bob", count: 2 });

    // total_trees
    expect(result.current.total_trees).toBe(2);
  });
});
