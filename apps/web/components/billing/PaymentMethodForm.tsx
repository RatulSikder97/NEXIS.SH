"use client";

// Phase 3.5 Stage 8 — Payment-method form.
//
// Plain HTML inputs + a small handful of validations on the card number
// (digits only, formatted with spaces every 4). The control-plane masks
// the card before storing it; we never persist the PAN client-side beyond
// the live form state.
//
// "Use test card" is a dev affordance — it fills with the conventional
// 4242 4242 4242 4242 dummy data so a tester can move past the form
// without touching real PII. Gated on NODE_ENV so it doesn't ship to
// production builds.

import * as React from "react";
import { Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { billing } from "@/lib/billing";

function formatCardNumber(raw: string): string {
  // Strip every non-digit, clamp to 19 digits (a Maestro outlier; most cards
  // are 13–16) and group by 4.
  const digits = raw.replace(/\D/g, "").slice(0, 19);
  return digits.replace(/(.{4})/g, "$1 ").trim();
}

export function PaymentMethodForm({
  onSaved,
}: {
  onSaved?: () => void;
}) {
  const router = useRouter();
  const [cardNumber, setCardNumber] = React.useState("");
  const [expMonth, setExpMonth] = React.useState("");
  const [expYear, setExpYear] = React.useState("");
  const [cvc, setCvc] = React.useState("");
  const [postal, setPostal] = React.useState("");
  const [email, setEmail] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const showDevHelpers = process.env.NODE_ENV !== "production";

  function fillTestCard() {
    setCardNumber("4242 4242 4242 4242");
    setExpMonth("12");
    setExpYear("30");
    setCvc("123");
    setPostal("10001");
    setEmail("test@example.com");
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await billing.addPaymentMethod({
        card_number: cardNumber.replace(/\s+/g, ""),
        exp_month: parseInt(expMonth, 10),
        // Two-digit year inputs become 20XX so a 2030 expiry doesn't
        // round-trip as the year 30.
        exp_year:
          expYear.length === 2 ? 2000 + parseInt(expYear, 10) : parseInt(expYear, 10),
        cvc,
        postal_code: postal,
        billing_email: email,
      });
      onSaved?.();
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save card");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <div className="space-y-1">
        <label
          htmlFor="card-number"
          className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
        >
          Card number
        </label>
        <input
          id="card-number"
          type="text"
          inputMode="numeric"
          autoComplete="cc-number"
          pattern="[\d ]*"
          required
          value={cardNumber}
          onChange={(e) => setCardNumber(formatCardNumber(e.target.value))}
          placeholder="4242 4242 4242 4242"
          className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 font-mono text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
        />
      </div>

      <div className="grid grid-cols-3 gap-3">
        <div className="space-y-1">
          <label
            htmlFor="card-exp-month"
            className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
          >
            Exp month
          </label>
          <input
            id="card-exp-month"
            type="text"
            inputMode="numeric"
            autoComplete="cc-exp-month"
            pattern="\d{1,2}"
            maxLength={2}
            required
            value={expMonth}
            onChange={(e) =>
              setExpMonth(e.target.value.replace(/\D/g, "").slice(0, 2))
            }
            placeholder="12"
            className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
          />
        </div>
        <div className="space-y-1">
          <label
            htmlFor="card-exp-year"
            className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
          >
            Exp year
          </label>
          <input
            id="card-exp-year"
            type="text"
            inputMode="numeric"
            autoComplete="cc-exp-year"
            pattern="\d{2,4}"
            maxLength={4}
            required
            value={expYear}
            onChange={(e) =>
              setExpYear(e.target.value.replace(/\D/g, "").slice(0, 4))
            }
            placeholder="30"
            className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
          />
        </div>
        <div className="space-y-1">
          <label
            htmlFor="card-cvc"
            className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
          >
            CVC
          </label>
          <input
            id="card-cvc"
            type="text"
            inputMode="numeric"
            autoComplete="cc-csc"
            pattern="\d{3,4}"
            maxLength={4}
            required
            value={cvc}
            onChange={(e) => setCvc(e.target.value.replace(/\D/g, "").slice(0, 4))}
            placeholder="123"
            className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
          />
        </div>
      </div>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
        <div className="space-y-1">
          <label
            htmlFor="card-postal"
            className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
          >
            Postal code
          </label>
          <input
            id="card-postal"
            type="text"
            autoComplete="postal-code"
            required
            value={postal}
            onChange={(e) => setPostal(e.target.value)}
            placeholder="10001"
            className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
          />
        </div>
        <div className="space-y-1">
          <label
            htmlFor="card-billing-email"
            className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
          >
            Billing email
          </label>
          <input
            id="card-billing-email"
            type="email"
            autoComplete="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="billing@example.com"
            className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
          />
        </div>
      </div>

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300"
        >
          {error}
        </div>
      )}

      <div className="flex items-center justify-between gap-3 pt-2">
        {showDevHelpers ? (
          <button
            type="button"
            onClick={fillTestCard}
            className="text-xs text-[var(--color-primary)] underline-offset-4 hover:underline"
          >
            Use test card
          </button>
        ) : (
          <span />
        )}
        <Button type="submit" disabled={busy}>
          {busy && <Loader2 className="h-4 w-4 animate-spin" />}
          Save payment method
        </Button>
      </div>
    </form>
  );
}
