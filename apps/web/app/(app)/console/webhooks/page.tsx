// Webhook activity surface — recent webhook deliveries.
//
// Server-side: try /v1/integrations/webhooks?limit=50. May 404 today; the
// client island handles "endpoint coming soon" empty state.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { WebhooksClient, type WebhookDelivery } from "./client";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

type WebhooksResp = {
  rows: WebhookDelivery[];
  total?: number;
};

export default async function WebhooksPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  let initial: WebhooksResp | null = null;
  try {
    const r = await fetch(`${API}/v1/integrations/webhooks?limit=50`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    });
    if (r.ok) {
      initial = (await r.json()) as WebhooksResp;
    }
  } catch {
    // ignore
  }

  return <WebhooksClient initial={initial} />;
}
