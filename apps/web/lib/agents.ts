// Agents fleet SDK — wraps GET /v1/workspaces/{ws}/agents.
// Phase 5 ships the 5 L1 agents; Phase 6 ships the 4 L2 agents + Approval Gate.

const API =
  typeof window === "undefined"
    ? process.env.API_URL_INTERNAL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080"
    : process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type AgentLayer = "l1" | "l2" | "router" | "detector";
export type AgentStatus = "available" | "degraded" | "disabled";

export type AgentInfo = {
  name: string;
  label: string;
  layer: AgentLayer;
  description: string;
  status: AgentStatus;
  recent_runs: number;
  last_seen_at?: string;
  total_tokens_in: number;
  total_tokens_out: number;
  total_cost_cents_exact: number;
  p50_duration_ms: number;
  p95_duration_ms: number;
};

export const agents = {
  list: async (wsId: string, cookie?: string): Promise<AgentInfo[]> => {
    const headers: Record<string, string> = { "content-type": "application/json" };
    if (cookie) headers.cookie = cookie;
    const r = await fetch(`${API}/v1/workspaces/${wsId}/agents`, {
      credentials: "include",
      headers,
      cache: "no-store",
    });
    if (!r.ok) return [];
    return r.json();
  },
};
