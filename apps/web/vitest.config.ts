import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "node",
    globals: true,
    // E2E specs live under tests/e2e/ and are driven by Playwright; vitest
    // would otherwise pick them up because they share the .spec.ts suffix.
    exclude: ["**/node_modules/**", "**/dist/**", "**/tests/e2e/**"],
  },
  resolve: {
    alias: {
      "@": new URL("./", import.meta.url).pathname,
      "@nexis/db": new URL("../../packages/db/index.ts", import.meta.url).pathname,
    },
  },
});
