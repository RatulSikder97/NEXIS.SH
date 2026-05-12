// Playwright configuration for the @nexis/web E2E suite. Tests are intended
// to run against a fully composed dev stack (docker compose up -d) — they
// exercise real signup, real cookies, real RLS, real audit. We force a single
// worker because the stack is shared state and parallel signups would race on
// the same DB/audit chain.
import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/e2e",
  timeout: 30_000,
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:3000",
    trace: "on-first-retry",
    screenshot: "only-on-failure",
  },
  workers: 1, // sequential — stack-shared state
  reporter: process.env.CI ? "github" : "list",
});
