"use client";

// Phase 3.5 Stage 8 / Phase 7 — Payment-method form.
//
// Two render branches gated on NEXT_PUBLIC_BILLING_PROVIDER:
//
//   - "stripe" → Stripe Elements (<CardElement /> inside <Elements>) that
//     pulls a SetupIntent client_secret from the control-plane, calls
//     `stripe.confirmCardSetup`, then POSTs the resulting payment_method id
//     back to the control-plane to attach it to the org's Stripe customer.
//     The PAN never touches our control-plane in this path.
//
//   - "local" (default) → Phase 3.5 mocked-card form below. Plain HTML inputs
//     with light client-side validation. The control-plane masks the card
//     before storing it; we never persist the PAN client-side beyond the
//     live form state.
//
// "Use test card" is a dev affordance — it fills with the conventional
// 4242 4242 4242 4242 dummy data so a tester can move past the form
// without touching real PII. Gated on NODE_ENV so it doesn't ship to
// production builds.

import * as React from "react";
import { Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { loadStripe, type Stripe } from "@stripe/stripe-js";
import {
  CardElement,
  Elements,
  useElements,
  useStripe,
} from "@stripe/react-stripe-js";

import { Button } from "@/components/ui/Button";
import { billing } from "@/lib/billing";

const BILLING_PROVIDER = process.env.NEXT_PUBLIC_BILLING_PROVIDER ?? "local";
const STRIPE_PUBLISHABLE_KEY = process.env.NEXT_PUBLIC_STRIPE_PUBLISHABLE_KEY ?? "";

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

function formatCardNumber(raw: string): string {
  // Strip every non-digit, clamp to 19 digits (a Maestro outlier; most cards
  // are 13–16) and group by 4.
  const digits = raw.replace(/\D/g, "").slice(0, 19);
  return digits.replace(/(.{4})/g, "$1 ").trim();
}

// `loadStripe` returns a singleton-style promise that must live outside of
// the component tree so we don't reload Stripe.js on every render.
let stripePromise: Promise<Stripe | null> | null = null;
function getStripePromise(): Promise<Stripe | null> | null {
  if (!STRIPE_PUBLISHABLE_KEY) return null;
  if (!stripePromise) {
    stripePromise = loadStripe(STRIPE_PUBLISHABLE_KEY);
  }
  return stripePromise;
}

export function PaymentMethodForm({
  onSaved,
}: {
  onSaved?: () => void;
}) {
  if (BILLING_PROVIDER === "stripe") {
    return <StripePaymentMethodForm onSaved={onSaved} />;
  }
  return <LocalPaymentMethodForm onSaved={onSaved} />;
}

// ---------- Stripe Elements path ----------

function StripePaymentMethodForm({ onSaved }: { onSaved?: () => void }) {
  const promise = getStripePromise();
  if (!promise) {
    return (
      <div
        role="alert"
        className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300"
      >
        Stripe publishable key is not configured. Set
        {" "}<code>NEXT_PUBLIC_STRIPE_PUBLISHABLE_KEY</code>.
      </div>
    );
  }
  return (
    <Elements stripe={promise}>
      <StripeInnerForm onSaved={onSaved} />
    </Elements>
  );
}

type SetupIntentResp = { client_secret: string };

function StripeInnerForm({ onSaved }: { onSaved?: () => void }) {
  const router = useRouter();
  const stripe = useStripe();
  const elements = useElements();

  const [email, setEmail] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);

    if (!stripe || !elements) {
      // Stripe.js hasn't finished loading yet — the button is disabled in
      // that state, but guard anyway.
      return;
    }
    const card = elements.getElement(CardElement);
    if (!card) {
      setError("Card element is not ready.");
      return;
    }

    setBusy(true);
    try {
      // 1. Ask the control-plane to mint a SetupIntent. The org's Stripe
      //    customer is created or reused server-side; we never see it.
      const intentRes = await fetch(`${API}/v1/billing/payment-method/intent`, {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ billing_email: email || undefined }),
      });
      if (!intentRes.ok) {
        const body = (await intentRes.json().catch(() => ({}))) as {
          error?: string;
        };
        throw new Error(body.error ?? intentRes.statusText);
      }
      const { client_secret } = (await intentRes.json()) as SetupIntentResp;
      if (!client_secret) {
        throw new Error("Server returned an empty SetupIntent secret.");
      }

      // 2. Confirm the SetupIntent in the browser. The PAN goes straight to
      //    Stripe — our origin never touches it.
      const confirm = await stripe.confirmCardSetup(client_secret, {
        payment_method: {
          card,
          billing_details: email ? { email } : undefined,
        },
      });
      if (confirm.error) {
        throw new Error(confirm.error.message ?? "Card was declined.");
      }
      const paymentMethodId = confirm.setupIntent?.payment_method;
      if (!paymentMethodId || typeof paymentMethodId !== "string") {
        throw new Error(
          "Stripe did not return a payment_method id; please retry.",
        );
      }

      // 3. Hand the payment_method id back to the control-plane, which
      //    attaches it to the customer + sets it as the default for invoices.
      const confirmRes = await fetch(
        `${API}/v1/billing/payment-method/confirm`,
        {
          method: "POST",
          credentials: "include",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            payment_method: paymentMethodId,
            billing_email: email || undefined,
          }),
        },
      );
      if (!confirmRes.ok) {
        const body = (await confirmRes.json().catch(() => ({}))) as {
          error?: string;
        };
        throw new Error(body.error ?? confirmRes.statusText);
      }

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
          htmlFor="stripe-card"
          className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
        >
          Card details
        </label>
        <div
          id="stripe-card"
          className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-3 text-sm focus-within:ring-2 focus-within:ring-[var(--color-ring)]"
        >
          <CardElement
            options={{
              hidePostalCode: false,
              style: {
                base: {
                  fontSize: "14px",
                  // Match the rest of the form's foreground colour; Stripe's
                  // CardElement can't read CSS variables itself.
                  color:
                    typeof window !== "undefined"
                      ? getComputedStyle(document.documentElement)
                          .getPropertyValue("--color-foreground")
                          .trim() || "#0f172a"
                      : "#0f172a",
                  "::placeholder": { color: "#9ca3af" },
                },
                invalid: { color: "#ef4444" },
              },
            }}
          />
        </div>
      </div>

      <div className="space-y-1">
        <label
          htmlFor="stripe-billing-email"
          className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
        >
          Billing email
        </label>
        <input
          id="stripe-billing-email"
          type="email"
          autoComplete="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="billing@example.com"
          className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
        />
      </div>

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300"
        >
          {error}
        </div>
      )}

      <div className="flex items-center justify-end gap-3 pt-2">
        <Button type="submit" disabled={busy || !stripe || !elements}>
          {busy && <Loader2 className="h-4 w-4 animate-spin" />}
          Save payment method
        </Button>
      </div>
    </form>
  );
}

// ---------- Phase 3.5 local mocked form ----------

function LocalPaymentMethodForm({ onSaved }: { onSaved?: () => void }) {
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
