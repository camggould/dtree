import { describe, it, expect, vi, beforeAll } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { DecisionDetail } from "@/components/DecisionDetail";
import type { Decision } from "@/api/types.gen";

// HeroUI Tabs uses ResizeObserver — stub it for jsdom
beforeAll(() => {
  if (typeof window.ResizeObserver === "undefined") {
    window.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

// Mock wouter (used by FilterPills and any child)
vi.mock("wouter", () => ({
  useSearch: () => ["", vi.fn()],
  useLocation: () => ["/", vi.fn()],
  Link: ({ children, href }: { children: React.ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
}));

// Mock the query module so we can control what useDecision returns
vi.mock("@/api/query", () => ({
  useDecision: vi.fn(),
  useHistory: vi.fn(),
  keys: {
    decision: (tree: string, id: string) => ["trees", tree, "decisions", id],
    decisions: (tree: string) => ["trees", tree, "decisions", {}],
  },
  queryClient: new QueryClient(),
}));

// Mock mutations to avoid real API calls
vi.mock("@/api/mutations", () => ({
  useDecide: () => ({ mutate: vi.fn(), isPending: false }),
  useUndecide: () => ({ mutate: vi.fn(), isPending: false }),
  useScopeOut: () => ({ mutate: vi.fn(), isPending: false }),
  useSupersede: () => ({ mutate: vi.fn(), isPending: false }),
  useRestore: () => ({ mutate: vi.fn(), isPending: false }),
  useRelate: () => ({ mutate: vi.fn(), isPending: false }),
  useUnrelate: () => ({ mutate: vi.fn(), isPending: false }),
  useUpdateDecision: () => ({ mutate: vi.fn(), isPending: false }),
  useDeleteDecision: () => ({ mutate: vi.fn(), isPending: false }),
}));

const seededDecision: Decision = {
  id: "dec-001",
  tree: "my-tree",
  summary: "Use PostgreSQL as primary datastore",
  description: "Evaluated MySQL, Postgres, and SQLite. Postgres wins for feature set.",
  priority: "high",
  status: "proposed",
  creator: "alice",
  tags: ["infrastructure", "database"],
  relationships: [],
  is_recommended: false,
  schema_version: 1,
  _rev: "rev-abc",
  created_at: "2025-01-01T00:00:00Z",
  updated_at: "2025-01-02T00:00:00Z",
};

describe("DecisionDetail", () => {
  it("renders Overview tab with seeded decision data", async () => {
    const { useDecision, useHistory } = await import("@/api/query");
    vi.mocked(useDecision).mockReturnValue({
      data: seededDecision,
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useDecision>);
    vi.mocked(useHistory).mockReturnValue({
      data: [],
      isLoading: false,
      isError: false,
    } as unknown as ReturnType<typeof useHistory>);

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    render(
      <QueryClientProvider client={qc}>
        <DecisionDetail tree="my-tree" id="dec-001" />
      </QueryClientProvider>,
    );

    // Should show the decision summary (appears in card header + overview body)
    expect(screen.getAllByText("Use PostgreSQL as primary datastore").length).toBeGreaterThan(0);

    // Should show description
    expect(
      screen.getByText(/Evaluated MySQL, Postgres, and SQLite/),
    ).toBeTruthy();

    // Should show status chip
    expect(screen.getByText("proposed")).toBeTruthy();

    // Should show priority chip
    expect(screen.getByText("high")).toBeTruthy();

    // Should show tags
    expect(screen.getByText("infrastructure")).toBeTruthy();
    expect(screen.getByText("database")).toBeTruthy();

    // Should show the Decide action button (proposed status)
    expect(screen.getByRole("button", { name: /Decide/i })).toBeTruthy();
  });
});
