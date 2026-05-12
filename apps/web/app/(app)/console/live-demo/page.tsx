// Phase 3 Stage 6 — Live Demo placeholder. Ships in Phase 6.

export default function LiveDemoPage() {
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <h1 className="text-2xl font-semibold">Live Demo</h1>
        <span className="rounded-full bg-[var(--color-accent)]/30 text-[var(--color-accent-foreground)] px-2 py-0.5 text-xs font-medium">
          Phase 6
        </span>
      </div>
      <p className="text-sm text-[var(--color-muted-foreground)] max-w-2xl">
        Interactive scripted scenario that walks a freshly-seeded org through an end-to-end incident detection → triage → recovery flow. Designed for design partner walkthroughs. Ships in Phase 6.
      </p>
    </div>
  );
}
