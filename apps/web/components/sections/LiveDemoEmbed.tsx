"use client";

import Link from "next/link";
import type { Route } from "next";
import { ArrowRight } from "lucide-react";

import { Button } from "@/components/ui/Button";

// Landing-page teaser for the synthetic-fault demo. The real pipeline canvas,
// scenario picker, and live log stream live under /console/live-demo — that
// route requires an authenticated session and a selected workspace, so the
// landing CTA is just a link there rather than a non-functional button that
// pretends to run a pipeline from the marketing surface.
export default function LiveDemoEmbed() {
  return (
    <section
      id="live-demo"
      className="bg-[var(--color-background)] py-24"
    >
      <div className="mx-auto max-w-[1200px] px-6">
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)] mb-3 text-center">
          LIVE PIPELINE
        </p>
        <h2 className="text-center text-3xl md:text-4xl font-semibold tracking-tight text-[var(--color-foreground)] mb-10">
          Run a synthetic incident.
        </h2>
        <div className="grid grid-cols-1 lg:grid-cols-[240px_1fr_400px] gap-4 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4 shadow-sm">
          <aside className="rounded-md bg-[var(--color-muted)] p-4">
            <h3 className="text-sm font-semibold mb-3 text-[var(--color-foreground)]">
              Scenarios
            </h3>
            <ul className="space-y-1 text-sm text-[var(--color-muted-foreground)]">
              {["Schema drift", "Null deref", "OOM", "Migration failure"].map(
                (s) => (
                  <li
                    key={s}
                    className="rounded px-2 py-1 hover:bg-[var(--color-card)] cursor-pointer"
                  >
                    {s}
                  </li>
                )
              )}
            </ul>
          </aside>
          <div className="min-h-[280px] rounded-md bg-[var(--color-muted)] grid place-items-center p-6 text-center text-sm text-[var(--color-muted-foreground)]">
            <div className="space-y-2">
              <p className="font-medium text-[var(--color-foreground)]">
                Pipeline canvas
              </p>
              <p className="max-w-[36ch]">
                Sign in to fire a synthetic fault and watch the recovery
                pipeline render every detect → diagnose → patch step in real
                time.
              </p>
            </div>
          </div>
          <aside className="rounded-md bg-[var(--color-foreground)] text-[var(--color-background)] font-mono text-xs p-4 overflow-auto">
            <div>$ nexis demo --scenario=schema-drift</div>
            <div className="opacity-60">[idle — open the console to run]</div>
          </aside>
        </div>
        <div className="mt-6 flex justify-center">
          <Button asChild size="lg">
            <Link href={"/console/live-demo" as Route}>
              Open the live demo
              <ArrowRight className="ml-2 h-4 w-4" />
            </Link>
          </Button>
        </div>
      </div>
    </section>
  );
}
