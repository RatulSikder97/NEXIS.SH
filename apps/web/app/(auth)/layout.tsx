import type { ReactNode } from "react";
import Link from "next/link";

import { ThemeAwareLogo } from "@/components/ThemeAwareLogo";

export default function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <main className="min-h-screen flex items-center justify-center bg-[var(--color-muted)] px-4">
      <div className="w-full max-w-md">
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
