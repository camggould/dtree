import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HeroUIProvider } from "@heroui/react";
import { KanbanView } from "@/views/KanbanView";
import type { PaginatedResponse, Decision } from "@/api/types.gen";

// Mock wouter
vi.mock("wouter", async () => {
  const actual = await vi.importActual<typeof import("wouter")>("wouter");
  return {
    ...actual,
    useParams: () => ({ tree: "test-tree" }),
    useLocation: () => ["/trees/test-tree/kanban", vi.fn()],
  };
});

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

const mockDecisions: PaginatedResponse<Decision> = {
  items: [
    {
      id: "dec-00000001",
      tree: "test-tree",
      summary: "Use PostgreSQL",
      priority: "high",
      status: "proposed",
      creator: "alice",
      is_recommended: false,
      schema_version: 1,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    },
    {
      id: "dec-00000002",
      tree: "test-tree",
      summary: "Adopt TypeScript",
      priority: "critical",
      status: "decided",
      creator: "bob",
      is_recommended: true,
      schema_version: 1,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    },
    {
      id: "dec-00000003",
      tree: "test-tree",
      summary: "Use MongoDB",
      priority: "low",
      status: "out_of_scope",
      creator: "charlie",
      is_recommended: false,
      schema_version: 1,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    },
    {
      id: "dec-00000004",
      tree: "test-tree",
      summary: "Use REST only",
      priority: "medium",
      status: "superseded",
      creator: "dave",
      is_recommended: false,
      schema_version: 1,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    },
  ],
};

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

describe("KanbanView", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: { get: () => null },
        json: () => Promise.resolve(mockDecisions),
      }),
    );
  });

  it("renders 4 columns with correct labels", async () => {
    render(<KanbanView />, { wrapper });

    expect(screen.getByText("Kanban Board")).toBeTruthy();

    // Wait for decisions to load and columns to render
    await screen.findByText("Use PostgreSQL");

    // All 4 column headers
    expect(screen.getByText("Proposed")).toBeTruthy();
    expect(screen.getByText("Decided")).toBeTruthy();
    expect(screen.getByText("Out of Scope")).toBeTruthy();
    expect(screen.getByText("Superseded")).toBeTruthy();
  });

  it("places decisions in the correct column", async () => {
    render(<KanbanView />, { wrapper });

    await screen.findByText("Use PostgreSQL");
    expect(screen.getByText("Adopt TypeScript")).toBeTruthy();
    expect(screen.getByText("Use MongoDB")).toBeTruthy();
    expect(screen.getByText("Use REST only")).toBeTruthy();
  });
});
