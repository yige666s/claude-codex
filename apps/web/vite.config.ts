import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

const apiTarget = process.env.AGENT_API_DEV_TARGET || "http://localhost:8081";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url))
    }
  },
  server: {
    port: 5173,
    proxy: {
      "/v1": apiTarget,
      "/healthz": apiTarget,
      "/readyz": apiTarget,
      "/metrics": apiTarget
    }
  },
  test: {
    environment: "node",
    include: ["src/**/*.{test,spec}.{ts,tsx}"]
  }
});
