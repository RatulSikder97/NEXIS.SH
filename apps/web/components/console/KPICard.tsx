// Phase 3 Stage 6 — KPI tile used on the console home page.
// Server component (no client state).

export function KPICard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
        {label}
      </p>
      <p className="text-3xl font-semibold mt-2">{value}</p>
    </div>
  );
}
