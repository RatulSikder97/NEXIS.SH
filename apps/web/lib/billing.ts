// Phase 3.5 Stage 8 — Billing SDK.
//
// Wraps the control-plane /v1/billing/* surfaces. All calls run with
// credentials: "include". Soft errors degrade to empty payloads where
// they don't matter (invoices, usage); the payment-method endpoints
// throw on non-OK so the form can surface server-side validation.

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type PaymentMethod = {
  id: string;
  org_id: string;
  provider: "local" | "stripe";
  brand?: string;
  last4?: string;
  exp_month?: number;
  exp_year?: number;
  billing_email?: string;
  created_at: string;
};

export type AddCardInput = {
  card_number: string;
  exp_month: number;
  exp_year: number;
  cvc: string;
  postal_code: string;
  billing_email: string;
};

export type Invoice = {
  id: string;
  period_start: string;
  period_end: string;
  total_cents: number;
  status: "open" | "paid" | "void";
  created_at: string;
};

export type UsageBreakdown = {
  total_cents: number;
  by_workspace: { key: string; total_cents: number }[];
  by_kind: { key: string; total_cents: number }[];
};

export const billing = {
  getPaymentMethod: async (): Promise<PaymentMethod | null> => {
    const r = await fetch(`${API}/v1/billing/payment-method`, {
      credentials: "include",
    });
    if (r.status === 404) return null;
    if (!r.ok) throw new Error(r.statusText);
    return r.json();
  },
  addPaymentMethod: async (input: AddCardInput): Promise<PaymentMethod> => {
    const r = await fetch(`${API}/v1/billing/payment-method`, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(input),
    });
    if (!r.ok) {
      const body = (await r.json().catch(() => ({}))) as { error?: string };
      throw new Error(body.error ?? r.statusText);
    }
    return r.json();
  },
  removePaymentMethod: async (): Promise<void> => {
    await fetch(`${API}/v1/billing/payment-method`, {
      method: "DELETE",
      credentials: "include",
    });
  },
  invoices: async (): Promise<Invoice[]> => {
    const r = await fetch(`${API}/v1/billing/invoices`, {
      credentials: "include",
    });
    if (!r.ok) return [];
    return r.json();
  },
  usage: async (since?: string, until?: string): Promise<UsageBreakdown> => {
    const u = new URL(`${API}/v1/billing/usage`);
    if (since) u.searchParams.set("since", since);
    if (until) u.searchParams.set("until", until);
    const r = await fetch(u.toString(), { credentials: "include" });
    if (!r.ok) return { total_cents: 0, by_workspace: [], by_kind: [] };
    return r.json();
  },
};
