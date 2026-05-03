import { create } from "zustand";
import { persist } from "zustand/middleware";

// Identity slice
interface IdentitySlice {
  currentHandle: string | null;
  setHandle: (handle: string | null) => void;
}

// Preferences slice
interface PrefsSlice {
  theme: "light" | "dark" | "system";
  setTheme: (theme: "light" | "dark" | "system") => void;
}

// Session slice
interface SessionSlice {
  lastTreeSlug: string | null;
  setLastTreeSlug: (slug: string | null) => void;
}

// Decision modal — opened from anywhere (graph, kanban, queue, audit, sidebar,
// drill-down list). Lifted to the store so a single <DecisionModal/> at the
// app root handles it; also keeps the modal alive across route changes.
interface DecisionModalSlice {
  decisionModal: { tree: string; id: string } | null;
  openDecision: (tree: string, id: string) => void;
  closeDecision: () => void;
}

interface AppStore
  extends IdentitySlice,
    PrefsSlice,
    SessionSlice,
    DecisionModalSlice {}

export const useAppStore = create<AppStore>()(
  persist(
    (set) => ({
      // Identity
      currentHandle: null,
      setHandle: (handle) => set({ currentHandle: handle }),

      // Prefs
      theme: "system",
      setTheme: (theme) => set({ theme }),

      // Session
      lastTreeSlug: null,
      setLastTreeSlug: (slug) => set({ lastTreeSlug: slug }),

      // Decision modal — not persisted (resets on reload)
      decisionModal: null,
      openDecision: (tree, id) => set({ decisionModal: { tree, id } }),
      closeDecision: () => set({ decisionModal: null }),
    }),
    {
      name: "dtree-app",
      partialize: (state) => ({
        currentHandle: state.currentHandle,
        theme: state.theme,
        lastTreeSlug: state.lastTreeSlug,
      }),
    },
  ),
);
