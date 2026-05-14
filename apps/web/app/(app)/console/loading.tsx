// Console-scoped loading skeleton. The console layout renders the Sidebar
// and Topbar synchronously, so this skeleton only stands in for the page
// body — three header rows plus a table-shaped block. Matches the visual
// rhythm of the dashboard / incidents / cost / eval pages so the swap to
// real content is a smooth dissolve, not a layout shift.

export default function ConsoleLoading() {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-live="polite"
      className="space-y-6"
    >
      <div className="flex items-center justify-between">
        <div className="space-y-2">
          <div className="h-7 w-48 animate-pulse rounded-md bg-[var(--color-muted)]" />
          <div className="h-4 w-72 animate-pulse rounded bg-[var(--color-muted)]" />
        </div>
        <div className="h-9 w-32 animate-pulse rounded-md bg-[var(--color-muted)]" />
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <div
            key={i}
            className="h-24 animate-pulse rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]"
          />
        ))}
      </div>

      <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
        <div className="mb-4 h-5 w-40 animate-pulse rounded bg-[var(--color-muted)]" />
        <div className="space-y-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <div
              key={i}
              className="h-10 animate-pulse rounded bg-[var(--color-muted)]"
            />
          ))}
        </div>
      </div>

      <span className="sr-only">Loading…</span>
    </div>
  );
}
