import { describe, it, expect, vi, beforeAll } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HeroUIProvider } from "@heroui/react";
import GraphView from "@/views/GraphView";
import type { Decision } from "@/api/types.gen";

// ---- Global stubs ----
beforeAll(() => {
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

  // ResizeObserver needed by ReactFlow
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});

// ---- Mock wouter ----
vi.mock("wouter", () => ({
  useParams: () => ({ tree: "test-tree" }),
  useLocation: () => ["/trees/test-tree", vi.fn()],
  Route: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  Switch: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

// ---- Mock useDecisions ----
const mockDecisions: Decision[] = [
  {
    id: "d1",
    tree: "test-tree",
    summary: "First decision about something important",
    priority: "high",
    status: "proposed",
    creator: "alice",
    is_recommended: false,
    schema_version: 1,
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    relationships: [{ type: "blocks", target: "d2" }],
  },
  {
    id: "d2",
    tree: "test-tree",
    summary: "Second decision depends on first",
    priority: "medium",
    status: "decided",
    creator: "bob",
    is_recommended: true,
    schema_version: 1,
    created_at: "2024-01-02T00:00:00Z",
    updated_at: "2024-01-02T00:00:00Z",
    relationships: [],
  },
];

vi.mock("@/api/query", () => ({
  useDecisions: () => ({
    data: { items: mockDecisions, next_cursor: undefined },
    isLoading: false,
    isError: false,
  }),
}));

function Wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={qc}>
      <HeroUIProvider>{children}</HeroUIProvider>
    </QueryClientProvider>
  );
}

describe("GraphView", () => {
  it("renders without crash with 2 decisions and 1 edge", () => {
    render(
      <Wrapper>
        <GraphView />
      </Wrapper>,
    );
    expect(document.body).toBeTruthy();
    // Toolbar direction buttons should be present
    expect(screen.getByText("Top→Bottom")).toBeTruthy();
    expect(screen.getByText("Left→Right")).toBeTruthy();
    expect(screen.getByText("Free")).toBeTruthy();
  });
});
