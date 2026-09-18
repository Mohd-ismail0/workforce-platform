import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
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
  test: { environment: "jsdom", setupFiles: [] },
});
