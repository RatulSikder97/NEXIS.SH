// Project detail loading skeleton.
//
// Mirrors the visual rhythm of `ProjectDetailClient` so the swap to real
// content is a smooth dissolve, not a layout shift:
//
//   • Back link row (small).
//   • Header: title + environment chip + meta.
//   • Tab pill bar (4 pills).
//   • KPI strip (2×2 on mobile, 1×4 on lg).
//   • Recent incidents card (header + 5 rows).
//
// The console layout already renders the Sidebar + Topbar synchronously,
// so this skeleton only stands in for the page body.

export default function ProjectDetailLoading() {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-live="polite"
      className="space-y-6"
    >
      <div className="h-4 w-32 animate-pulse rounded bg-[var(--color-muted)]" />

      <header className="space-y-3">
        <div className="flex flex-wrap items-center gap-3">
          <div className="h-7 w-64 animate-pulse rounded-md bg-[var(--color-muted)]" />
          <div className="h-5 w-16 animate-pulse rounded-full bg-[var(--color-muted)]" />
        </div>
        <div className="h-4 w-96 max-w-full animate-pulse rounded bg-[var(--color-muted)]" />
      </header>

      <div className="flex flex-wrap items-center gap-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <div
            key={i}
            className="h-8 w-28 animate-pulse rounded-full bg-[var(--color-muted)]"
          />
        ))}
      </div>

      <section aria-label="Project KPIs">
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <div
              key={i}
              className="h-24 animate-pulse rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]"
            />
          ))}
        </div>
      </section>

      <section className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5">
        <div className="mb-4 flex items-baseline justify-between gap-3">
          <div className="h-5 w-40 animate-pulse rounded bg-[var(--color-muted)]" />
          <div className="h-4 w-20 animate-pulse rounded bg-[var(--color-muted)]" />
        </div>
        <div className="space-y-3">
          {Array.from({ length: 5 }).map((_, i) => (
            <div
              key={i}
              className="h-10 animate-pulse rounded bg-[var(--color-muted)]"
            />
          ))}
        </div>
      </section>

      <span className="sr-only">Loading project detail…</span>
    </div>
  );
}
