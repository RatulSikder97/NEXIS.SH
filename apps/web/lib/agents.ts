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

// AgentRunSeverity mirrors the gate-derived severity bucket the control
// plane writes onto activity_events.payload.severity. "unknown" covers
// the pre-classification case (Synthesiser hasn't run yet or the row
// belongs to a non-recovery workflow).
export type AgentRunSeverity = "low" | "medium" | "high" | "unknown";

// AgentRunStatus is the agent-scoped lifecycle bucket. The control-plane
// projects the underlying workflow_run + activity_event status onto one
// of these four states so the drill-down doesn't have to know about
// retries / timed_out / cancelled in its own UI.
export type AgentRunStatus = "succeeded" | "failed" | "running" | "degraded";

// AgentRun is one row of the per-agent run log. `finished_at` is absent
// while the agent activity is still in flight; `summary_message` mirrors
// the message we'd display on the timeline row for that finish frame.
export type AgentRun = {
  run_id: string;
  scenario: string;
  severity: AgentRunSeverity;
  status: AgentRunStatus;
  started_at: string;
  finished_at?: string;
  duration_ms: number;
  tokens_in: number;
  tokens_out: number;
  cost_cents_exact: number;
  event_count: number;
  degraded: boolean;
  summary_message?: string;
};

export type AgentRunsResp = {
  runs: AgentRun[];
  total: number;
};

// ActivityEventResp is the wire shape of one row in activity_events as
// served by the per-agent drill-down endpoint. `payload` is the full
// JSONB blob so the admin viewer can render tool_calls / decisions /
// model_name / etc verbatim.
export type ActivityEventResp = {
  id: string;
  workflow_run_id: string;
  agent_role: string;
  kind: "start" | "log" | "finish" | "error";
  ts: string;
  message: string;
  payload: Record<string, unknown>;
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

  // runs returns the per-agent run log for the given workspace + agent
  // name. Pagination is offset-based; the server bounds limit and returns
  // a `{runs, total}` envelope so the client knows when to disable the
  // "Load more" button. Server-side callers must pass a cookie header
  // because Next 16's fetch does not forward credentials by default.
  runs: async (
    wsId: string,
    name: string,
    opts?: { limit?: number; offset?: number; cookie?: string },
  ): Promise<AgentRunsResp> => {
    const limit = opts?.limit ?? 50;
    const offset = opts?.offset ?? 0;
    const headers: Record<string, string> = { "content-type": "application/json" };
    if (opts?.cookie) headers.cookie = opts.cookie;
    const r = await fetch(
      `${API}/v1/workspaces/${wsId}/agents/${name}/runs?limit=${limit}&offset=${offset}`,
      {
        credentials: "include",
        headers,
        cache: "no-store",
      },
    );
    if (!r.ok) return { runs: [], total: 0 };
    return r.json();
  },

  // events returns every persisted activity_event for one (agent, run_id)
  // pair, ordered by ts. Used by the inline timeline viewer to render
  // start/log/finish/error rows with the full payload blob. Empty array
  // on any non-2xx so the UI degrades to "no events" instead of throwing.
  events: async (
    wsId: string,
    name: string,
    runId: string,
    cookie?: string,
  ): Promise<ActivityEventResp[]> => {
    const headers: Record<string, string> = { "content-type": "application/json" };
    if (cookie) headers.cookie = cookie;
    const r = await fetch(
      `${API}/v1/workspaces/${wsId}/agents/${name}/runs/${runId}/events`,
      {
        credentials: "include",
        headers,
        cache: "no-store",
      },
    );
    if (!r.ok) return [];
    return r.json();
  },
};
