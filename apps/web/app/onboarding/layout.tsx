// Phase 3.5 Stage 6 — Onboarding shell.
//
// Bare centered-card layout, intentionally divorced from the console
// chrome (Sidebar + Topbar). New tenants land here straight from signup
// or invite-accept and need a focus mode — no nav, no breadcrumbs.

import type { ReactNode } from "react";
import Link from "next/link";

import { ThemeAwareLogo } from "@/components/ThemeAwareLogo";

export default function OnboardingLayout({ children }: { children: ReactNode }) {
  return (
    <main className="min-h-screen flex items-center justify-center bg-[var(--color-muted)] px-4 py-12">
      <div className="w-full max-w-2xl">
        <div className="flex justify-center mb-8">
          <Link href="/">
            <ThemeAwareLogo />
          </Link>
        </div>
        <div className="bg-[var(--color-card)] border border-[var(--color-border)] rounded-xl shadow-sm p-8">
          {children}
        </div>
      </div>
    </main>
  );
}
