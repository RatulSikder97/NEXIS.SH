// System health surface — 4-column status grid + integration health board.
//
// Server-side: try /v1/system-health (may 404 today). Always also pull
// /v1/integrations so the integration half is always populated. Both halves
// degrade independently — a 404 on system-health renders the EmptyState
// card alongside the live integration board.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { HealthClient, type SubsystemRow } from "./client";
import {
  integrations as integrationsApi,
  type IntegrationConnection,
} from "@/lib/integrations";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

type SystemHealthResp = {
  rows: SubsystemRow[];
};

export default async function HealthPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  let subsystems: SubsystemRow[] | null = null;
  try {
    const r = await fetch(`${API}/v1/system-health`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    });
    if (r.ok) {
      const body = (await r.json()) as SystemHealthResp;
      subsystems = body.rows ?? [];
    }
  } catch {
    // ignore
  }

  let connections: IntegrationConnection[] = [];
  try {
    const r = await fetch(`${API}/v1/integrations`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    });
    if (r.ok) {
      const body = (await r.json()) as unknown[];
      // Re-use the SDK's normaliser by calling listConnections client-side,
      // but the server fetch needs the cookie — we replicate the minimal
      // normalisation inline here.
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
    // ignore
  }
  void integrationsApi;

  return <HealthClient subsystems={subsystems} connections={connections} />;
}
