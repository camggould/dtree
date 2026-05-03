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

interface AppStore extends IdentitySlice, PrefsSlice, SessionSlice {}

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
