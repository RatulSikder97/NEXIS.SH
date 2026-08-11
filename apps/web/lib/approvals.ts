// Phase 6 Stage 9 — Approvals SDK.
//
// Wraps the control-plane's approval HTTP surface for the console UI:
//
//   GET   /v1/workspaces/{ws}/approvals/pending           → owner|admin list
//   GET   /v1/workspaces/{ws}/pipelines/{run}/decision    → single decision
//   POST  /v1/workspaces/{ws}/pipelines/{run}/approve     → owner|admin write
//
// JSON shapes mirror the Phase 6 spec (snake_case). Severity drives the
// pill colour in the table; `awaiting_decision_since` drives the relative
// "Awaiting Xm" column. The pending entry carries a small map of per-agent
// summaries — the table shows the Synthesiser confidence verbatim and the
// detail page can show the rest if/when we link out.
//
// All requests carry the session cookie via `credentials: "include"` so
// the workspace-scope RLS binding works exactly like the pipelines SDK.

const API =
  typeof window === "undefined"
    ? (process.env.API_URL_INTERNAL ??
      process.env.NEXT_PUBLIC_API_URL ??
      "http://localhost:8080")
    : (process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080");

// Severity classes match the Phase 6 approval.Classify Go-side enum.
export type ApprovalSeverity = "low" | "medium" | "high";

// ApprovalDecisionState covers every terminal + non-terminal state the
// workflow can park the decision row in. The UI cares most about
// `pending` (offer approve/reject) vs everything else (read-only).
export type ApprovalDecisionState =
  | "pending"
  | "approve"
  | "reject"
  | "approved"
  | "rejected"
  | "modified"
  | "auto"
  | "auto_approved"
  | "timed_out"
  | "timeout_rejected";

// ApprovalDecision is the wire shape of GET /pipelines/{run}/decision.
// Aligned with the user-supplied contract: `id`, `run_id`, `severity`,
// `decision`, `decided_by`, `decided_at`, `notes`, `awaiting_since`.
// Optional snake_case fields (`workspace_id`, `risk_score`, `created_at`)
// are present in the Phase 6 spec's broader shape — we read them when
// the backend exposes them and ignore otherwise.
export type ApprovalDecision = {
  id: string;
  run_id: string;
  workspace_id?: string;
  severity: ApprovalSeverity;
  decision: ApprovalDecisionState;
  decided_by: string | null;
  decided_at: string | null;
  notes: string | null;
  awaiting_since: string | null;
  scenario?: string | null;
  risk_score?: number | null;
  created_at?: string | null;
};

// AgentSummary is a compact freeform field — the backend currently
// surfaces `confidence` for the Synthesiser. We type it loosely so future
// keys (rationale, top_factors, …) don't require an SDK change.
export type AgentSummary = {
  confidence?: number;
  rationale?: string;
  [key: string]: unknown;
};

// PendingApproval is the wire shape of GET /workspaces/{ws}/approvals/pending
// — subset of a WorkflowRun joined with the approval row + severity.
export type PendingApproval = {
  run_id: string;
  workspace_id: string;
  scenario: string | null;
  severity: ApprovalSeverity;
  awaiting_decision_since: string;
  agent_summaries: {
    Sentinel?: AgentSummary;
    Pathfinder?: AgentSummary;
    Synthesiser?: AgentSummary;
    Validator?: AgentSummary;
    [agent: string]: AgentSummary | undefined;
  };
};

// failOr drains a non-2xx response for the server-supplied error string and
// throws with the message. 202 is treated as success — the approval handler
// returns 202 to signal "signal accepted, workflow is processing async".
async function failOr<T>(r: Response): Promise<T | undefined> {
  if (!r.ok && r.status !== 202) {
    const body = (await r
      .json()
      .catch(() => ({}) as Record<string, unknown>)) as {
      error?: string;
    };
    throw new Error(body.error ?? r.statusText);
  }
  // 202 may have an empty body — bail out without parsing.
  if (r.status === 202) {
    try {
      return (await r.json()) as T;
    } catch {
      return undefined;
    }
  }
  return r.json() as Promise<T>;
}

// approvals is the symmetric counterpart to lib/pipelines.ts. We name the
// methods after the verbs in the spec (pending / decision / approve / reject)
// so the call sites read like the §13.1 route table.
export const approvals = {
  // pending returns every approval_decisions row for `wsId` with
  // `decision='pending'`, joined with the workflow_runs metadata + the
  // Phase 5 agent summaries. The backend orders newest-first.
  pending: async (wsId: string): Promise<PendingApproval[]> => {
    const r = await fetch(`${API}/v1/workspaces/${wsId}/approvals/pending`, {
      credentials: "include",
      cache: "no-store",
    });
    const out = await failOr<PendingApproval[]>(r);
    return out ?? [];
  },

  // decision returns the single ApprovalDecision row for the given run.
  // 404 → null so callers can render an "approval not gated yet" state
  // without try/catching at the call site.
  decision: async (
    wsId: string,
    runId: string,
  ): Promise<ApprovalDecision | null> => {
    const r = await fetch(
      `${API}/v1/workspaces/${wsId}/pipelines/${runId}/decision`,
      { credentials: "include", cache: "no-store" },
    );
    if (r.status === 404) return null;
    const out = await failOr<ApprovalDecision>(r);
    return out ?? null;
  },

  // approve and reject share the underlying POST shape; the only difference
  // is the `decision` payload field. We expose two narrow methods so call
  // sites don't have to remember the literal string.
  approve: async (
    wsId: string,
    runId: string,
    notes?: string,
  ): Promise<void> => {
    const r = await fetch(
      `${API}/v1/workspaces/${wsId}/pipelines/${runId}/approve`,
      {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ decision: "approve", notes: notes ?? "" }),
      },
    );
    await failOr<void>(r);
  },

  reject: async (
    wsId: string,
    runId: string,
    notes?: string,
  ): Promise<void> => {
    const r = await fetch(
      `${API}/v1/workspaces/${wsId}/pipelines/${runId}/reject`,
      {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ notes: notes ?? "" }),
      },
    );
    await failOr<void>(r);
  },

  // modify is the RLHF "modify-then-approve" write: the engineer submits an
  // edited unified diff which the workflow deploys INSTEAD of the agent's
  // original, and the (original, edited) pair is recorded as a training
  // example. `modifiedDiff` is required — the backend 400s without it.
  modify: async (
    wsId: string,
    runId: string,
    modifiedDiff: string,
    notes?: string,
  ): Promise<void> => {
    const r = await fetch(
      `${API}/v1/workspaces/${wsId}/pipelines/${runId}/modify`,
      {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          notes: notes ?? "",
          modified_diff: modifiedDiff,
        }),
      },
    );
    await failOr<void>(r);
  },
};

// SEVERITY_LABEL renders the severity enum as a short noun for the table.
export const SEVERITY_LABEL: Record<ApprovalSeverity, string> = {
  low: "Low",
  medium: "Medium",
  high: "High",
};
