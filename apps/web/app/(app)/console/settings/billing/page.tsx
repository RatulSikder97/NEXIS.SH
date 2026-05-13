// Phase 3.5 Stage 8 — Settings/Billing.
//
// Server component shell. Fetches the org's payment method (may be null),
// invoices, and current-period usage in parallel using the request's
// nexis_session cookie. Hands the snapshot off to the client for
// add/remove flows.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { BillingClient } from "./client";
import type {
  Invoice,
  PaymentMethod,
  UsageBreakdown,
} from "@/lib/billing";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

async function fetchPaymentMethod(
  cookieHeader: string,
): Promise<PaymentMethod | null> {
  const r = await fetch(`${API}/v1/billing/payment-method`, {
    headers: { cookie: cookieHeader },
    cache: "no-store",
  });
  if (r.status === 404 || !r.ok) return null;
  return (await r.json()) as PaymentMethod;
}

async function fetchInvoices(cookieHeader: string): Promise<Invoice[]> {
  const r = await fetch(`${API}/v1/billing/invoices`, {
    headers: { cookie: cookieHeader },
    cache: "no-store",
  });
  if (!r.ok) return [];
  return (await r.json()) as Invoice[];
}

async function fetchUsage(cookieHeader: string): Promise<UsageBreakdown> {
  const r = await fetch(`${API}/v1/billing/usage`, {
    headers: { cookie: cookieHeader },
    cache: "no-store",
  });
  if (!r.ok) return { total_cents: 0, by_workspace: [], by_kind: [] };
  return (await r.json()) as UsageBreakdown;
}

export default async function BillingPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  const [pm, invoices, usage] = await Promise.all([
    fetchPaymentMethod(cookieHeader),
    fetchInvoices(cookieHeader),
    fetchUsage(cookieHeader),
  ]);

  return <BillingClient pm={pm} invoices={invoices} usage={usage} />;
}
