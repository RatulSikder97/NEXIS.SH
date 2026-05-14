// Incident (pipeline run) detail loading skeleton.
//
// Mirrors the visual rhythm of `TimelineClient` so the swap is a smooth
// dissolve:
//
//   • Back link.
//   • Header: run id + status pill + timestamps row.
//   • Agent summary + confidence card.
//   • Activity timeline (vertical list of 8 event rows with a left rail).

export default function IncidentDetailLoading() {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-live="polite"
      className="space-y-6"
    >
      <div className="h-4 w-24 animate-pulse rounded bg-[var(--color-muted)]" />

      <header className="space-y-3">
        <div className="flex flex-wrap items-center gap-3">
          <div className="h-7 w-72 animate-pulse rounded-md bg-[var(--color-muted)]" />
          <div className="h-5 w-20 animate-pulse rounded-full bg-[var(--color-muted)]" />
        </div>
        <div className="flex flex-wrap gap-4">
          <div className="h-3 w-36 animate-pulse rounded bg-[var(--color-muted)]" />
          <div className="h-3 w-32 animate-pulse rounded bg-[var(--color-muted)]" />
          <div className="h-3 w-28 animate-pulse rounded bg-[var(--color-muted)]" />
        </div>
      </header>

      <section className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {Array.from({ length: 2 }).map((_, i) => (
          <div
            key={i}
            className="h-28 animate-pulse rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]"
          />
        ))}
      </section>

      <section className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5">
        <div className="mb-4 h-5 w-40 animate-pulse rounded bg-[var(--color-muted)]" />
        <ol className="relative space-y-3">
          {Array.from({ length: 8 }).map((_, i) => (
            <li
              key={i}
              className="flex items-start gap-3 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] p-3"
            >
              <div className="h-5 w-16 shrink-0 animate-pulse rounded-full bg-[var(--color-muted)]" />
              <div className="flex-1 space-y-2">
                <div className="h-3 w-2/3 animate-pulse rounded bg-[var(--color-muted)]" />
                <div className="h-3 w-1/3 animate-pulse rounded bg-[var(--color-muted)]" />
              </div>
            </li>
          ))}
        </ol>
      </section>

      <span className="sr-only">Loading incident detail…</span>
    </div>
  );
}
