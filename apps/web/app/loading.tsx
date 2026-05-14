// Root loading skeleton. Rendered while the root route's RSC tree is being
// fetched. Kept intentionally minimal — most landing-page routes are static
// and resolve quickly; this is the safety net for the cases where they don't.

export default function RootLoading() {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-live="polite"
      className="flex min-h-[60vh] items-center justify-center px-6 py-24"
    >
      <div className="flex w-full max-w-[640px] flex-col gap-6">
        <div className="h-10 w-2/3 animate-pulse rounded-md bg-[var(--color-muted)]" />
        <div className="h-4 w-full animate-pulse rounded bg-[var(--color-muted)]" />
        <div className="h-4 w-5/6 animate-pulse rounded bg-[var(--color-muted)]" />
        <div className="h-4 w-3/4 animate-pulse rounded bg-[var(--color-muted)]" />
        <div className="mt-4 flex gap-3">
          <div className="h-11 w-32 animate-pulse rounded-md bg-[var(--color-muted)]" />
          <div className="h-11 w-28 animate-pulse rounded-md bg-[var(--color-muted)]" />
        </div>
        <span className="sr-only">Loading…</span>
      </div>
    </div>
  );
}
