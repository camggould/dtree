import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import App from "@/App";

// Minimal mocks needed for rendering without a real DOM environment
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

// Mock EventSource (used by useAuditStream)
class MockEventSource {
  onopen: (() => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: (() => void) | null = null;
  close() {}
}
vi.stubGlobal("EventSource", MockEventSource);

// Mock fetch for query client
vi.stubGlobal("fetch", vi.fn().mockResolvedValue({
  ok: true,
  status: 200,
  headers: { get: () => null },
  json: () => Promise.resolve({ trees: [] }),
}));

describe("App", () => {
  it("renders without crashing", () => {
    render(<App />);
    // App should render (even if data is loading)
    // Check that the root element renders some content
    expect(document.body).toBeTruthy();
  });

  it("renders dtree brand text", () => {
    render(<App />);
    const brand = screen.getAllByText("dtree");
    expect(brand.length).toBeGreaterThan(0);
  });
});
