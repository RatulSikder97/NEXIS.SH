"use client";

// Phase 3.5 Stage 8 — Billing settings client.
//
// Three stacked sections:
//   1. Payment method — saved card summary (with Remove) OR the add-card form
//   2. Usage          — total this period + per-workspace / per-kind bars
//   3. Invoices       — table of period closures
//
// Remove triggers router.refresh() so the next render flips back to the
// add-card form.

import * as React from "react";
import { Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { PaymentMethodForm } from "@/components/billing/PaymentMethodForm";
import { InvoicesTable } from "@/components/billing/InvoicesTable";
import { UsageSummary } from "@/components/billing/UsageSummary";
import { billing, type Invoice, type PaymentMethod, type UsageBreakdown } from "@/lib/billing";

function formatExp(month: number | undefined, year: number | undefined): string {
  if (!month || !year) return "—";
  const m = String(month).padStart(2, "0");
  // Year arrives as full four-digit from the control-plane; show the last two
  // to match standard card-on-file UX.
  const y = String(year).slice(-2);
  return `${m}/${y}`;
}

function SavedCard({
  pm,
  onRemove,
  removing,
}: {
  pm: PaymentMethod;
  onRemove: () => void;
  removing: boolean;
}) {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Card on file
          </p>
          <p className="mt-1 font-mono text-sm">
            {pm.brand ? `${pm.brand} ` : ""}•••• •••• •••• {pm.last4 ?? "????"}
          </p>
          <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
            Expires {formatExp(pm.exp_month, pm.exp_year)} ·{" "}
            {pm.billing_email ?? ""}
          </p>
        </div>
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={removing}
          onClick={onRemove}
        >
          {removing && <Loader2 className="h-4 w-4 animate-spin" />}
          Remove
        </Button>
      </div>
    </div>
  );
}

export function BillingClient({
  pm,
  invoices,
  usage,
}: {
  pm: PaymentMethod | null;
  invoices: Invoice[];
  usage: UsageBreakdown;
}) {
  const router = useRouter();
  const [removing, setRemoving] = React.useState(false);

  async function remove() {
    if (!confirm("Remove the saved card? You'll need to re-add one before next cycle.")) {
      return;
    }
    setRemoving(true);
    try {
      await billing.removePaymentMethod();
      router.refresh();
    } finally {
      setRemoving(false);
    }
  }

  return (
    <div className="space-y-10">
      <div>
        <h1 className="text-2xl font-semibold">Billing</h1>
        <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
          Manage your payment method, review usage, and download invoices.
        </p>
      </div>

      <section className="space-y-3">
        <h2 className="text-sm font-medium uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Payment method
        </h2>
        {pm ? (
          <SavedCard pm={pm} onRemove={remove} removing={removing} />
        ) : (
          <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5">
            <PaymentMethodForm />
          </div>
        )}
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-medium uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Usage
        </h2>
        <UsageSummary usage={usage} />
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-medium uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Invoices
        </h2>
        <InvoicesTable invoices={invoices} />
      </section>
    </div>
  );
}
