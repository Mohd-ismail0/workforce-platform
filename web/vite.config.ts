import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    // The UI is organised by feature, so imports read as "@/features/inbox/inbox-page"
    // instead of a chain of ../../.. that breaks whenever a file moves.
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    host: "127.0.0.1",
    proxy: {
      // /auth MUST be proxied. Without it Vite answers with its own HTML fallback, so an
      // unauthenticated /auth/session looks like a 200 success and the UI renders a
      // logged-in shell over a request that never reached the API.
      "/auth": "http://127.0.0.1:8095",
      "/api": "http://127.0.0.1:8095",
      "/health": "http://127.0.0.1:8095",
      "/ready": "http://127.0.0.1:8095",
    },
  },
  test: { environment: "jsdom", setupFiles: ["./src/test-setup.ts"] },
});
