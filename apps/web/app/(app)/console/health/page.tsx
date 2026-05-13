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

// The BE returns a flat object: {control_plane:{status,latency_ms,checked_at,last_error?}, postgres:{...}, redis:{...}, neo4j:{...}, minio:{...}, temporal:{...}}.
type Check = {
  status: "healthy" | "degraded" | "down" | "disabled";
  latency_ms: number;
  checked_at: string;
  last_error?: string;
};
type SystemHealthResp = {
  control_plane?: Check;
  postgres?: Check;
  redis?: Check;
  neo4j?: Check;
  minio?: Check;
  temporal?: Check;
};

const SUBSYSTEM_LABELS: Array<[keyof SystemHealthResp, string]> = [
  ["control_plane", "Control plane"],
  ["postgres", "Postgres"],
  ["redis", "Redis"],
  ["temporal", "Temporal"],
  ["minio", "MinIO"],
  ["neo4j", "Neo4j"],
];

function toSubsystemRows(body: SystemHealthResp): SubsystemRow[] {
  return SUBSYSTEM_LABELS.map(([key, label]) => {
    const c = body[key];
    if (!c) {
      return { key, label, state: "unknown" } satisfies SubsystemRow;
    }
    const state: SubsystemRow["state"] =
      c.status === "healthy"
        ? "healthy"
        : c.status === "degraded"
          ? "degraded"
          : c.status === "down"
            ? "down"
            : "unknown";
    return {
      key,
      label,
      state,
      latency_ms: c.latency_ms,
      last_check_at: c.checked_at,
      last_error: c.last_error,
      detail: c.status === "disabled" ? "Probe disabled in this environment" : undefined,
    } satisfies SubsystemRow;
  });
}

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
      subsystems = toSubsystemRows(body);
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
