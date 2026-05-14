// Agent detail loading skeleton.
//
// Mirrors the visual rhythm of `AgentDetailPage` so the swap is a smooth
// dissolve:
//
//   • Back link row (small).
//   • Header: agent label + layer chip + status dot + monospace name + description.
//   • Stat strip (2×2 on mobile, 1×4 on md).
//   • Run log card (header + 6 rows).

export default function AgentDetailLoading() {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-live="polite"
      className="space-y-6"
    >
      <div className="h-4 w-24 animate-pulse rounded bg-[var(--color-muted)]" />

      <header className="space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <div className="h-7 w-56 animate-pulse rounded-md bg-[var(--color-muted)]" />
          <div className="h-4 w-12 animate-pulse rounded bg-[var(--color-muted)]" />
          <div className="h-4 w-20 animate-pulse rounded bg-[var(--color-muted)]" />
        </div>
        <div className="h-3 w-40 animate-pulse rounded bg-[var(--color-muted)]" />
        <div className="space-y-1.5">
          <div className="h-3 w-full max-w-2xl animate-pulse rounded bg-[var(--color-muted)]" />
          <div className="h-3 w-3/4 max-w-2xl animate-pulse rounded bg-[var(--color-muted)]" />
        </div>
      </header>

      <section
        aria-label="Agent statistics"
        className="grid grid-cols-2 gap-3 md:grid-cols-4"
      >
        {Array.from({ length: 4 }).map((_, i) => (
          <div
            key={i}
            className="h-20 animate-pulse rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]"
          />
        ))}
      </section>

      <section className="space-y-3">
        <div className="flex items-end justify-between gap-3">
          <div className="space-y-2">
            <div className="h-5 w-32 animate-pulse rounded bg-[var(--color-muted)]" />
            <div className="h-3 w-64 animate-pulse rounded bg-[var(--color-muted)]" />
          </div>
          <div className="h-3 w-20 animate-pulse rounded bg-[var(--color-muted)]" />
        </div>
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
          <div className="space-y-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <div
                key={i}
                className="h-10 animate-pulse rounded bg-[var(--color-muted)]"
              />
            ))}
          </div>
        </div>
      </section>

      <span className="sr-only">Loading agent detail…</span>
    </div>
  );
}
