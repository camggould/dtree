import "@testing-library/jest-dom";

// jsdom doesn't ship ResizeObserver; xyflow and recharts call it. A no-op
// stub is sufficient for our smoke tests — none of them exercise resize
// behaviour.
class ResizeObserverShim {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}
if (typeof globalThis.ResizeObserver === "undefined") {
  (globalThis as { ResizeObserver?: typeof ResizeObserver }).ResizeObserver =
    ResizeObserverShim as unknown as typeof ResizeObserver;
}

// xyflow's bundled CSS reads window.matchMedia at module load.
if (typeof window !== "undefined" && !window.matchMedia) {
  window.matchMedia = (() => ({
    matches: false,
    media: "",
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
}
