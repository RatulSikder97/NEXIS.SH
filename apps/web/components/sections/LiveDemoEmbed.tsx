"use client";

import { Button } from "@/components/ui/Button";

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
          <div className="min-h-[280px] rounded-md bg-[var(--color-muted)] grid place-items-center text-sm text-[var(--color-muted-foreground)]">
            Pipeline canvas (Phase 6 wiring)
          </div>
          <aside className="rounded-md bg-[var(--color-foreground)] text-[var(--color-background)] font-mono text-xs p-4 overflow-auto">
            <div>$ nexis demo --scenario=schema-drift</div>
            <div className="opacity-60">[idle — click Run]</div>
          </aside>
        </div>
        <div className="mt-6 text-center">
          <Button>Run a synthetic incident</Button>
        </div>
      </div>
    </section>
  );
}
