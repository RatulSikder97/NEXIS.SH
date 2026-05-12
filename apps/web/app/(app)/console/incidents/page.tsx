// Phase 3 Stage 6 — Incidents placeholder. Ships in Phase 4.

export default function IncidentsPage() {
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <h1 className="text-2xl font-semibold">Incidents</h1>
        <span className="rounded-full bg-[var(--color-accent)]/30 text-[var(--color-accent-foreground)] px-2 py-0.5 text-xs font-medium">
          Phase 4
        </span>
      </div>
      <p className="text-sm text-[var(--color-muted-foreground)] max-w-2xl">
        Real-time incident table with severity, service, owner, and agent timeline. Ships in Phase 4 along with the Temporal-backed recovery pipeline.
      </p>
    </div>
  );
}
