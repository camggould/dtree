import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HeroUIProvider } from "@heroui/react";
import { Dashboard } from "@/views/Dashboard";
import type { Metrics, Tree } from "@/api/types.gen";

// Stub matchMedia for HeroUI
vi.stubGlobal("matchMedia", (query: string) => ({
  matches: false,
  media: query,
  onchange: null,
  addListener: vi.fn(),
  removeListener: vi.fn(),
  addEventListener: vi.fn(),
  removeEventListener: vi.fn(),
  dispatchEvent: vi.fn(),
}));

// Stub EventSource
class MockEventSource {
  onopen: (() => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: (() => void) | null = null;
  close() {}
}
vi.stubGlobal("EventSource", MockEventSource);

const mockTrees: Tree[] = [
  {
    slug: "alpha",
    schema_version: 1,
    name: "Alpha",
    archived: false,
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  },
];

const mockMetrics: Metrics = {
  total_decisions: 10,
  by_status: {
    proposed: 4,
    decided: 5,
    out_of_scope: 1,
    superseded: 0,
  },
  by_priority: {
    assumption: 1,
    low: 2,
    medium: 4,
    high: 2,
    critical: 1,
  },
  by_creator: [
    { handle: "alice", count: 6 },
    { handle: "bob", count: 4 },
  ],
  assumptions_count: 1,
  unblocked_proposed_count: 3,
  oldest_proposed_id: "d-001",
};

const mockDecisions = {
  items: [
    {
      id: "d-001",
      tree: "alpha",
      summary: "Use PostgreSQL",
      priority: "high" as const,
      status: "decided" as const,
      creator: "alice",
      is_recommended: true,
      created_at: "2025-01-01T00:00:00Z",
      updated_at: "2025-01-02T00:00:00Z",
      schema_version: 1,
    },
    {
      id: "d-002",
      tree: "alpha",
      summary: "Deploy to k8s",
      priority: "medium" as const,
      status: "decided" as const,
      creator: "bob",
      is_recommended: false,
      actual_choice: "docker-compose",
      recommended_summary: "kubernetes",
      created_at: "2025-01-01T00:00:00Z",
      updated_at: "2025-01-03T00:00:00Z",
      schema_version: 1,
    },
  ],
};

const mockAudit = { items: [] };

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={qc}>
      <HeroUIProvider>{children}</HeroUIProvider>
    </QueryClientProvider>
  );
}

describe("Dashboard", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) => {
        if (url.includes("/v1/trees") && !url.includes("/metrics") && !url.includes("/decisions")) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: { get: () => null },
            json: () => Promise.resolve({ trees: mockTrees }),
          });
        }
        if (url.includes("/metrics")) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: { get: () => null },
            json: () => Promise.resolve(mockMetrics),
          });
        }
        if (url.includes("/decisions")) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: { get: () => null },
            json: () => Promise.resolve(mockDecisions),
          });
        }
        if (url.includes("/audit")) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: { get: () => null },
            json: () => Promise.resolve(mockAudit),
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

  it("renders Dashboard heading", () => {
    render(<Dashboard />, { wrapper });
    expect(screen.getByText("Dashboard")).toBeTruthy();
  });

  it("renders section headings", () => {
    render(<Dashboard />, { wrapper });
    expect(screen.getByText("Recommendation Acceptance")).toBeTruthy();
    expect(screen.getByText("Status Breakdown")).toBeTruthy();
    expect(screen.getByText("Priority Breakdown")).toBeTruthy();
    expect(screen.getByText(/Activity/)).toBeTruthy();
    expect(screen.getByText("Top Contributors")).toBeTruthy();
  });
});
