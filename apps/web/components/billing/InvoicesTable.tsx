// Phase 3.5 Stage 8 — Invoices table.
//
// Server-renderable presentation component. Renders one row per invoice
// with the billing period (Mon YYYY derived from period_start), the
// total in USD, and the status badge. Empty state nudges the user that
// invoices appear at cycle close.

import type { Invoice } from "@/lib/billing";

function formatPeriod(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: "short",
      year: "numeric",
    });
  } catch {
    return iso;
  }
}

function formatUSD(cents: number): string {
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: "USD",
  }).format(cents / 100);
}

const STATUS_STYLE: Record<Invoice["status"], string> = {
  paid: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 border-emerald-500/30",
  open: "bg-amber-500/10 text-amber-700 dark:text-amber-300 border-amber-500/30",
  void: "bg-gray-500/10 text-gray-600 dark:text-gray-400 border-gray-500/30",
};

export function InvoicesTable({ invoices }: { invoices: Invoice[] }) {
  if (invoices.length === 0) {
    return (
      <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] px-4 py-8 text-center text-sm text-[var(--color-muted-foreground)]">
        No invoices yet — they&apos;ll appear at the end of your first billing cycle.
      </div>
    );
  }
  return (
    <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
      <table className="w-full text-sm">
        <thead className="bg-[var(--color-muted)]/40 text-left text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          <tr>
            <th className="px-4 py-2 font-medium">Period</th>
            <th className="px-4 py-2 font-medium">Total</th>
            <th className="px-4 py-2 font-medium">Status</th>
          </tr>
        </thead>
        <tbody>
          {invoices.map((inv) => (
            <tr key={inv.id} className="border-t border-[var(--color-border)]">
              <td className="px-4 py-3">{formatPeriod(inv.period_start)}</td>
              <td className="px-4 py-3 font-mono">{formatUSD(inv.total_cents)}</td>
              <td className="px-4 py-3">
                <span
                  className={`inline-flex items-center rounded-md border px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ${STATUS_STYLE[inv.status]}`}
                >
                  {inv.status}
                </span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
