import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import path from "node:path";

export default defineConfig({
  plugins: [react()],
  test: {
    // The DOM is installed explicitly by vitest.setup.ts through happy-dom's
    // global registrator rather than through Vitest's `environment` option,
    // which does not resolve a DOM environment in this project.
    environment: "node",
    globals: true,
    setupFiles: ["./vitest.setup.ts"],
    include: ["**/*.test.{ts,tsx}"],
    exclude: ["node_modules/**", ".next/**"],
  },
  resolve: {
    alias: { "@": path.resolve(import.meta.dirname, ".") },
  },
});
