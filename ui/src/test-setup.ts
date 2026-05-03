import "@testing-library/jest-dom";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

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

// Unmount React trees between tests so framer-motion's LazyMotion doesn't
// fire a setState after jsdom is torn down. LazyMotion (pulled in by
// HeroUIProvider) dynamically imports its feature bundle inside a useEffect
// and calls setState when the import resolves. Without explicit cleanup the
// microtask races jsdom teardown on slower CI runners and throws an
// unhandled "window is not defined" rejection — every assertion still
// passes but vitest exits 1 because of the late rejection. The combination
// of cleanup() and the config-level dangerouslyIgnoreUnhandledErrors flag
// (see vite.config.ts) covers both fast and slow runners.
afterEach(() => {
  cleanup();
});
