// Phase 4 Stage 7 — Pipelines SDK.
//
// Wraps the control-plane's pipeline HTTP surface for the console UI:
//
//   GET    /v1/workspaces/{ws}/pipelines           → list runs
//   GET    /v1/workspaces/{ws}/pipelines/{id}      → single run + activity events
//   POST   /v1/workspaces/{ws}/pipelines           → start a RecoveryPipeline
//   POST   /v1/workspaces/{ws}/pipelines/{demo}    → start a demo (triggered_by=demo) run
//   GET    /v1/workspaces/{ws}/pipelines/{id}/events  → SSE stream of ActivityEvent frames
//
// The JSON shapes here mirror the Go DTOs verbatim (snake_case). Field
// nullability mirrors the `omitempty` tags so the client doesn't have to
// guard against the empty string everywhere.
//
// SSE protocol contract (see PipelineEvents in control-plane/handler/pipelines.go):
//   • Data frames are `data: <ActivityEventResp JSON>\n\n` — one per event.
//   • The server replays everything in the DB first, then tails the live broker.
//   • Termination is a named `event: close\ndata: {}\n\n` after the terminal
//     `agent_role: "pipeline"` row. Listen for "close" via addEventListener.
//   • The browser will auto-reconnect on transient errors; we don't override
//     that — once the named "close" event fires, we close the EventSource
//     deliberately so it does NOT reconnect.

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

// WorkflowRunStatus tracks the lifecycle of a single pipeline run. Mirrors
// domain.WorkflowRunStatus on the Go side.
export type WorkflowRunStatus =
  | "queued"
  | "running"
  | "succeeded"
  | "failed"
  | "timed_out"
  | "cancelled";

// WorkflowRun is the wire shape of one row from the pipelines list / detail
// endpoints. `current_step`, `completed_at`, `duration_ms`, and `error`
// are omitted by the server when empty/nil — we use `?:` to match.
export type WorkflowRun = {
  id: string;
  org_id: string;
  workspace_id: string;
  workflow_type: string;
  status: WorkflowRunStatus;
  current_step?: string;
  started_at: string;
  completed_at?: string;
  duration_ms?: number;
  error?: string;
};

// ActivityEventStatus tracks per-activity lifecycle within a run.
export type ActivityEventStatus =
  | "started"
  | "succeeded"
  | "failed"
  | "retrying"
  | "timed_out";

// ActivityEvent is the wire shape of one row in activity_events. `payload`
// is opaque JSON; the timeline UI reads structured fields out of it for
// activity-specific badges (e.g. Backend.Codegen → {tests_passed, ...}).
export type ActivityEvent = {
  workflow_run_id: string;
  seq: number;
  agent_role: string;
  activity_name: string;
  status: ActivityEventStatus;
  attempt: number;
  message?: string;
  payload?: Record<string, unknown>;
  ts: string;
};

// PipelineDetail is the response of GET /v1/workspaces/{ws}/pipelines/{id}.
// The field name `events` matches the Go DTO — not `activity_events`.
export type PipelineDetail = {
  run: WorkflowRun;
  events: ActivityEvent[];
};

// AGENTS_IN_ORDER drives the timeline rendering. The names match the
// domain.AgentRole values emitted by the workflow + activities — keep this
// in lockstep with the Go side when new agents are introduced. The final
// row ("pipeline") is the terminal Pipeline.Complete event.
export const AGENTS_IN_ORDER = [
  "sentinel",
  "pathfinder",
  "synthesiser",
  "architect",
  "backend",
  "qa",
  "devops",
  "data_engineer",
  "approval_gate",
] as const;

export type AgentRole = (typeof AGENTS_IN_ORDER)[number] | "pipeline";

// AGENT_LABELS gives the timeline a human-readable label per role. We don't
// look these up at render time from a server response so this needs to track
// the agent fleet manually — but the set is small and stable.
export const AGENT_LABELS: Record<AgentRole, string> = {
  sentinel: "Sentinel · Detect",
  pathfinder: "Pathfinder · Diagnose",
  synthesiser: "Synthesiser · Plan",
  architect: "Architect · Solution",
  backend: "Backend · Codegen",
  qa: "QA · TestGen",
  devops: "DevOps · Pipeline",
  data_engineer: "Data Engineer · Migrations",
  approval_gate: "Approval Gate · Route",
  pipeline: "Pipeline · Complete",
};

async function failOr<T>(r: Response): Promise<T> {
  if (!r.ok) {
    const body = (await r.json().catch(() => ({}) as Record<string, unknown>)) as {
      error?: string;
    };
    throw new Error(body.error ?? r.statusText);
  }
  return r.json() as Promise<T>;
}

export const pipelines = {
  // list returns the latest workflow_runs rows for a workspace. The server
  // bounds limit to [1,200] with a default of 50.
  list: async (wsId: string, limit = 50): Promise<WorkflowRun[]> => {
    const r = await fetch(
      `${API}/v1/workspaces/${wsId}/pipelines?limit=${limit}`,
      { credentials: "include", cache: "no-store" },
    );
    return failOr<WorkflowRun[]>(r);
  },

  // get returns the run snapshot + every persisted activity_event ordered by
  // seq. Used to seed the timeline before opening the SSE stream.
  get: async (wsId: string, runId: string): Promise<PipelineDetail> => {
    const r = await fetch(`${API}/v1/workspaces/${wsId}/pipelines/${runId}`, {
      credentials: "include",
      cache: "no-store",
    });
    return failOr<PipelineDetail>(r);
  },

  // create kicks off a RecoveryPipeline and returns the workflow_run row
  // (status will be "queued" or "running" depending on how fast Temporal
  // accepts the start). The browser is responsible for following the SSE.
  create: async (
    wsId: string,
    input: Record<string, unknown> = {},
  ): Promise<WorkflowRun> => {
    const r = await fetch(`${API}/v1/workspaces/${wsId}/pipelines`, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ workflow_type: "RecoveryPipeline", input }),
    });
    return failOr<WorkflowRun>(r);
  },

  // createDemo posts to the demo subroute, which stamps triggered_by=demo on
  // the workflow input so the dashboard can filter demo runs from real ones.
  // Server gates this on app_env=dev; in prod it returns 404.
  createDemo: async (
    wsId: string,
    input: Record<string, unknown> = {},
  ): Promise<WorkflowRun> => {
    const r = await fetch(`${API}/v1/workspaces/${wsId}/pipelines/demo`, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(input),
    });
    return failOr<WorkflowRun>(r);
  },

  // events opens an EventSource and parses incoming `data:` frames as
  // ActivityEvent. The returned handle should be closed by the caller on
  // unmount; the helper also closes it itself when the named "close" event
  // fires (which is the server's terminal signal), so the EventSource doesn't
  // auto-reconnect to a finished stream.
  events: (
    wsId: string,
    runId: string,
    onEvent: (e: ActivityEvent) => void,
    onClose?: () => void,
  ): EventSource => {
    const es = new EventSource(
      `${API}/v1/workspaces/${wsId}/pipelines/${runId}/events`,
      { withCredentials: true },
    );
    es.onmessage = (ev) => {
      try {
        const parsed = JSON.parse(ev.data) as ActivityEvent;
        onEvent(parsed);
      } catch {
        // ignore malformed frames + keep-alives (": ping")
      }
    };
    es.addEventListener("close", () => {
      es.close();
      onClose?.();
    });
    return es;
  },
};
