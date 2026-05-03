import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HeroUIProvider } from "@heroui/react";
import { QueueView } from "@/views/QueueView";
import type { Decision, QueueItem } from "@/api/types.gen";

// Mock wouter — kind will be overridden per-test
let mockKind = "quick-wins";

vi.mock("wouter", async () => {
  const actual = await vi.importActual<typeof import("wouter")>("wouter");
  return {
    ...actual,
    useParams: () => ({ tree: "test-tree", kind: mockKind }),
    useLocation: () => ["/", vi.fn()],
    Link: ({ href, children, className }: { href: string; children: React.ReactNode; className?: string }) => (
      <a href={href} className={className}>{children}</a>
    ),
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

const mockQuickWins: { items: Decision[] } = {
  items: [
    {
      id: "dec-00000001",
      tree: "test-tree",
      summary: "Use Redis for caching",
      priority: "medium",
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
      summary: "Adopt ESLint flat config",
      priority: "low",
      status: "proposed",
      creator: "bob",
      is_recommended: false,
      schema_version: 1,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    },
  ],
};

const mockSpearhead: { items: QueueItem[] } = {
  items: [
    {
      id: "dec-00000010",
      summary: "Choose authentication strategy",
      blocking_count: 5,
    },
    {
      id: "dec-00000011",
      summary: "Decide on database ORM",
      blocking_count: 3,
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

describe("QueueView — quick-wins", () => {
  beforeEach(() => {
    mockKind = "quick-wins";
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: { get: () => null },
        json: () => Promise.resolve(mockQuickWins),
      }),
    );
  });

  it("renders quick-wins cards", async () => {
    render(<QueueView />, { wrapper });

    expect(screen.getByText("Quick Wins Queue")).toBeTruthy();

    const card1 = await screen.findByText("Use Redis for caching");
    expect(card1).toBeTruthy();

    const card2 = await screen.findByText("Adopt ESLint flat config");
    expect(card2).toBeTruthy();

    // Rank numbers should appear
    expect(screen.getByText("1")).toBeTruthy();
    expect(screen.getByText("2")).toBeTruthy();

    // "Open detail" links
    const links = screen.getAllByText("Open detail");
    expect(links.length).toBe(2);
  });
});

describe("QueueView — spearhead", () => {
  beforeEach(() => {
    mockKind = "spearhead";
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: { get: () => null },
        json: () => Promise.resolve(mockSpearhead),
      }),
    );
  });

  it("renders spearhead cards with blocking_count", async () => {
    render(<QueueView />, { wrapper });

    expect(screen.getByText("Spearhead Queue")).toBeTruthy();

    const card1 = await screen.findByText("Choose authentication strategy");
    expect(card1).toBeTruthy();

    // blocking_count chips
    const blocking5 = await screen.findByText("Blocking 5");
    expect(blocking5).toBeTruthy();

    const blocking3 = await screen.findByText("Blocking 3");
    expect(blocking3).toBeTruthy();
  });
});
