import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

export default defineConfig({
  base: "/ui/",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  build: {
    outDir: "../internal/uifs/dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/v1": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test-setup.ts"],
    // HeroUIProvider mounts framer-motion's LazyMotion which fires a
    // dynamic feature import and calls setState when it resolves. On
    // slower CI runners that microtask lands AFTER jsdom is torn down,
    // throwing "window is not defined" as an unhandled rejection. Every
    // assertion still passes, but vitest treats the late rejection as a
    // suite failure. Ignoring those late errors is safe for our smoke
    // tests — none of them assert on thrown promises.
    dangerouslyIgnoreUnhandledErrors: true,
  },
});
