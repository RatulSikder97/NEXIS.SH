// Phase 3.5 Stage 8 — Usage summary card.
//
// Pure presentation. Total this period as a big number; a stacked bar
// chart by workspace; then a smaller "by kind" breakdown. Bars are pure
// Tailwind divs sized as a percent of the largest entry — no chart lib.

import type { UsageBreakdown } from "@/lib/billing";

// Render USD with adaptive precision: ≥$0.01 shows 2 decimals; sub-cent
// shows up to 4 so a few minutes of $0.10/hr metering surfaces as
// "$0.0021" rather than "$0.00".
function formatUSD(cents: number): string {
  const dollars = cents / 100;
  const decimals = Math.abs(dollars) >= 0.01 || dollars === 0 ? 2 : 4;
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(dollars);
}

function valueOf(r: { total_cents: number; total_cents_exact?: number }): number {
  return r.total_cents_exact ?? r.total_cents;
}

function Bars({
  rows,
  emptyLabel,
}: {
  rows: { key: string; total_cents: number; total_cents_exact?: number }[];
  emptyLabel: string;
}) {
  if (rows.length === 0) {
    return (
      <p className="text-xs text-[var(--color-muted-foreground)]">{emptyLabel}</p>
    );
  }
  const max = Math.max(...rows.map(valueOf), 1);
  return (
    <ul className="space-y-2">
      {rows.map((r) => {
        const v = valueOf(r);
        const pct = Math.round((v / max) * 100);
        return (
          <li key={r.key} className="space-y-1">
            <div className="flex items-baseline justify-between text-xs">
              <span className="truncate font-mono text-[var(--color-foreground)]">
                {r.key}
              </span>
              <span className="text-[var(--color-muted-foreground)]">
                {formatUSD(v)}
              </span>
            </div>
            <div
              className="h-1.5 w-full overflow-hidden rounded-full bg-[var(--color-muted)]"
              role="presentation"
            >
              <div
                className="h-full rounded-full bg-[var(--color-primary)]"
                style={{ width: `${pct}%` }}
              />
            </div>
          </li>
        );
      })}
    </ul>
  );
}

export function UsageSummary({ usage }: { usage: UsageBreakdown }) {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5">
      <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
        Usage this period
      </p>
      <p className="mt-1 text-3xl font-semibold text-[var(--color-foreground)]">
        {formatUSD(usage.total_cents_exact ?? usage.total_cents)}
      </p>

      <div className="mt-5 space-y-5">
        <section className="space-y-2">
          <h3 className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            By workspace
          </h3>
          <Bars rows={usage.by_workspace} emptyLabel="No workspace usage yet." />
        </section>

        <section className="space-y-2">
          <h3 className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            By kind
          </h3>
          <Bars rows={usage.by_kind} emptyLabel="No metered events yet." />
        </section>
      </div>
    </div>
  );
}
