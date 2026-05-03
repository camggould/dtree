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
//
// `stack` records what was open before the current modal so a related-decision
// click can swap the modal contents and "Back" returns to the previous one.
interface DecisionModalSlice {
  decisionModal: { tree: string; id: string } | null;
  decisionStack: { tree: string; id: string }[];
  /** Open without remembering the previous modal (entry from outside). */
  openDecision: (tree: string, id: string) => void;
  /** Open from inside an open modal; current is pushed onto stack. */
  pushDecision: (tree: string, id: string) => void;
  /** Pop the most recent stacked modal (Back). */
  popDecision: () => void;
  /** Close everything and reset the stack. */
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
      decisionStack: [],
      openDecision: (tree, id) =>
        set({ decisionModal: { tree, id }, decisionStack: [] }),
      pushDecision: (tree, id) =>
        set((s) => ({
          decisionModal: { tree, id },
          decisionStack: s.decisionModal
            ? [...s.decisionStack, s.decisionModal]
            : s.decisionStack,
        })),
      popDecision: () =>
        set((s) => {
          if (s.decisionStack.length === 0) {
            return { decisionModal: null, decisionStack: [] };
          }
          const previous = s.decisionStack[s.decisionStack.length - 1];
          return {
            decisionModal: previous,
            decisionStack: s.decisionStack.slice(0, -1),
          };
        }),
      closeDecision: () =>
        set({ decisionModal: null, decisionStack: [] }),
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
