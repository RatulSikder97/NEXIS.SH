// Connection health surface — the 6 integration cards with probe-now affordance.
//
// Server side: pull /v1/integrations and pass the connections to the client
// island. The client exposes a "Probe now" button per provider that POSTs
// to /v1/integrations/{provider}/probe — endpoint may 404; we surface the
// error inline.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { ConnectionsClient } from "./client";
import type { IntegrationConnection } from "@/lib/integrations";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function ConnectionsPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  let connections: IntegrationConnection[] = [];
  try {
    const r = await fetch(`${API}/v1/integrations`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    });
    if (r.ok) {
      const body = (await r.json()) as unknown[];
      connections = body.map((raw) => {
        const o = raw as Partial<IntegrationConnection> & {
          status?: "connected" | "disconnected" | "error" | "pending";
        };
        const connected =
          typeof o.connected === "boolean"
            ? o.connected
            : o.status === "connected";
        return {
          provider: (o.provider ?? "github") as IntegrationConnection["provider"],
          connected,
          health: o.health ?? {
            state: connected ? "unknown" : "disconnected",
          },
          status: o.status,
          installation_id: o.installation_id,
          metadata: o.metadata,
          last_error: o.last_error,
          created_at: o.created_at,
          updated_at: o.updated_at,
        };
      });
    }
  } catch {
    // ignore — client renders the empty state
  }

  return <ConnectionsClient initial={connections} />;
}
