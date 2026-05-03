import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HeroUIProvider } from "@heroui/react";
import { AuditView } from "@/views/AuditView";
import type { AuditResponse } from "@/api/types.gen";

// Mock wouter
vi.mock("wouter", async () => {
  const actual = await vi.importActual<typeof import("wouter")>("wouter");
  return {
    ...actual,
    useParams: () => ({ tree: "test-tree" }),
    useSearch: () => "",
  };
});

// Mock EventSource
class MockEventSource {
  onopen: (() => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: (() => void) | null = null;
  close() {}
}
vi.stubGlobal("EventSource", MockEventSource);

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

const mockAuditResponse: AuditResponse = {
  items: [
    {
      event_id: "evt-001",
      v: 1,
      ts: new Date(Date.now() - 60000).toISOString(),
      actor: "alice",
      action: "create",
      kind: "decision",
      tree: "test-tree",
      id: "dec-00000001",
      payload: {
        after: { summary: "Use PostgreSQL for storage" },
      },
    },
    {
      event_id: "evt-002",
      v: 1,
      ts: new Date(Date.now() - 120000).toISOString(),
      actor: "bob",
      action: "decide",
      kind: "decision",
      tree: "test-tree",
      id: "dec-00000002",
      payload: {
        after: { summary: "Adopt TypeScript everywhere" },
      },
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

describe("AuditView", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: { get: () => null },
        json: () => Promise.resolve(mockAuditResponse),
      }),
    );
  });

  it("renders audit table with mocked events", async () => {
    render(<AuditView />, { wrapper });

    // Heading should render immediately
    expect(screen.getByText("Audit Log")).toBeTruthy();

    // Wait for data to load (spinner disappears, table appears)
    const aliceCell = await screen.findByText("alice");
    expect(aliceCell).toBeTruthy();

    const bobCell = await screen.findByText("bob");
    expect(bobCell).toBeTruthy();

    // Table columns should be visible after load
    // (use getAllByText since some labels also appear in filter inputs)
    expect(screen.getAllByText("Time").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Actor").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Action").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Kind").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Ref").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Summary").length).toBeGreaterThan(0);
  });

  it("renders live tail toggle button", () => {
    render(<AuditView />, { wrapper });
    const btn = screen.getByText(/live tail/i);
    expect(btn).toBeTruthy();
  });
});
