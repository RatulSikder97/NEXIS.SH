// Wraps every public marketing page so users always have an escape route
// (navbar back to landing, footer to all resources). Server-component so the
// Navbar's client hydration doesn't gate first paint.
import type { ReactNode } from "react";

import Footer from "@/components/layout/Footer";
import Navbar from "@/components/layout/Navbar";

export function MarketingShell({ children }: { children: ReactNode }) {
  return (
    <>
      <Navbar />
      <main className="bg-[var(--color-background)]">{children}</main>
      <Footer />
    </>
  );
}
