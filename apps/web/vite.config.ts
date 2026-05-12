import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

const apiTarget = process.env.AGENT_API_DEV_TARGET || "http://localhost:8081";

export default defineConfig({
  plugins: [react()],
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
