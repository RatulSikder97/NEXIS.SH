// Shared helpers for the Playwright e2e suite.
//
// These run against a live `docker compose up -d` stack — real signups,
// real cookies, real RLS. Everything in here is a small composable
// utility callers can lift into a test without pulling in the rest.

import { generateSync } from "otplib";

// totpNow returns a fresh TOTP code for `secret`, using the same RFC 6238
// strategy our control-plane verifies against. Centralised so individual
// spec files don't reach for `otplib` directly — keeps the dependency on
// one TOTP library + one strategy choice ("totp") in one place.
export function totpNow(secret: string): string {
  return generateSync({ secret, strategy: "totp" });
}
