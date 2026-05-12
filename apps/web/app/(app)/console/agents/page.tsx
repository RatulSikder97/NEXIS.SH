// Phase 3 Stage 6 — Agents placeholder. Ships in Phase 5.

export default function AgentsPage() {
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <h1 className="text-2xl font-semibold">Agents</h1>
        <span className="rounded-full bg-[var(--color-accent)]/30 text-[var(--color-accent-foreground)] px-2 py-0.5 text-xs font-medium">
          Phase 5
        </span>
      </div>
      <p className="text-sm text-[var(--color-muted-foreground)] max-w-2xl">
        Agent registry, health, and capability matrix. Inspect each agent&apos;s recent runs, tool permissions, and current backlog. Ships in Phase 5 with the multi-agent recovery orchestration layer.
      </p>
    </div>
  );
}
