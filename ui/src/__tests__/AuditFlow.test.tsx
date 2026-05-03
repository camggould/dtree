import { describe, it, expect, vi, beforeAll } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HeroUIProvider } from "@heroui/react";
import AuditFlow from "@/components/AuditFlow";
import type { Event } from "@/api/types.gen";

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

  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});

// ---- Mock useHistory ----
const mockEvents: Event[] = [
  {
    event_id: "e1",
    v: 1,
    ts: "2024-01-01T10:00:00Z",
    actor: "alice",
    action: "create",
    kind: "decision",
    tree: "test-tree",
    id: "d1",
    payload: {},
  },
  {
    event_id: "e2",
    v: 1,
    ts: "2024-01-02T10:00:00Z",
    actor: "bob",
    action: "update",
    kind: "decision",
    tree: "test-tree",
    id: "d1",
    payload: {},
  },
  {
    event_id: "e3",
    v: 1,
    ts: "2024-01-03T10:00:00Z",
    actor: "alice",
    action: "decide",
    kind: "decision",
    tree: "test-tree",
    id: "d1",
    payload: {},
  },
];

vi.mock("@/api/query", () => ({
  useHistory: () => ({
    data: { events: mockEvents },
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

describe("AuditFlow", () => {
  it("renders with 3 mocked history events", () => {
    render(
      <Wrapper>
        <AuditFlow tree="test-tree" id="d1" />
      </Wrapper>,
    );
    expect(document.body).toBeTruthy();
    // Actor names should appear in the rendered nodes
    const aliceNodes = screen.getAllByText("alice");
    expect(aliceNodes.length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("bob")).toBeTruthy();
  });
});
