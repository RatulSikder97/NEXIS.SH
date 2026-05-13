// Phase 5 Stage 8 — Eval SDK.
//
// Wraps the control-plane's eval HTTP surface for the /console/eval UI:
//
//   GET  /v1/workspaces/{ws}/eval                → list summaries
//   GET  /v1/workspaces/{ws}/eval/{run_id}       → full detail (run + transcripts)
//   POST /v1/workspaces/{ws}/eval                → kick off a new run
//   GET  /v1/workspaces/{ws}/agents/budget       → token-budget pill data
//
// Field names mirror the Go DTOs verbatim (snake_case). Status string union
// is what the workflow + eval runner persist; the UI never invents values.
// Costs are surfaced as cents-exact (integer fractional cents × 10000 in
// the *_cents_exact fields, matching the Phase 3.5 billing precision lesson)
// so the table can render sub-cent runs without rounding to "$0.00".

const API =
  typeof window === "undefined"
    ? process.env.API_URL_INTERNAL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080"
    : process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

// EvalRunStatus tracks each provider's leg of an eval run. The runner
// records one status per provider (openai / ollama) so the matrix can show
// independent failure modes — e.g. ollama failed schema validation while
// openai succeeded.
export type EvalRunStatus = "queued" | "running" | "succeeded" | "failed";

// EvalProvider is the per-transcript provider tag. Phase 5 ships exactly
// two; Phase 6+ may add Anthropic / etc.
export type EvalProvider = "openai" | "ollama";

// EvalAgent is the L1 agent that produced a transcript. Order matters when
// rendering: Architect → Backend → QA → DevOps → DataEngineer.
export type EvalAgent =
  | "Architect"
  | "Backend"
  | "QA"
  | "DevOps"
  | "DataEngineer";

// AGENTS_IN_ORDER drives the side-by-side transcript rendering. Five rows,
// one per L1 agent, in the order the workflow invokes them.
export const AGENTS_IN_ORDER: readonly EvalAgent[] = [
  "Architect",
  "Backend",
  "QA",
  "DevOps",
  "DataEngineer",
] as const;

// EvalRunSummary is the list-view row shape. Costs are emitted in
// fractional cents (cents × 10000) so the UI does the precision math once,
// in formatCents below, instead of every consumer dividing by 10000.
export type EvalRunSummary = {
  id: string;
  scenario: string;
  openai_status: EvalRunStatus;
  ollama_status: EvalRunStatus;
  openai_cost_cents_exact: number;
  ollama_cost_cents_exact: number;
  openai_tokens_in: number;
  openai_tokens_out: number;
  ollama_tokens_in: number;
  ollama_tokens_out: number;
  duration_openai_ms: number;
  duration_ollama_ms: number;
  started_at: string;
  finished_at?: string;
};

// EvalTranscript is one (provider × agent) cell. `input_json` is the
// rendered prompt + retrieval context as JSON; `output_json` is the
// structured assistant output (validated against the agent's schema when
// `success` is true). We render both as pretty-printed JSON in Phase 5;
// Phase 7 swaps in Monaco diff view.
export type EvalTranscript = {
  id: string;
  eval_run_id: string;
  provider: EvalProvider;
  agent: EvalAgent;
  input_json: Record<string, unknown> | unknown[] | null;
  output_json: Record<string, unknown> | unknown[] | null;
  success: boolean;
  tokens_in: number;
  tokens_out: number;
  cached_tokens: number;
  cost_cents_exact: number;
  duration_ms: number;
  started_at: string;
  finished_at: string;
};

// EvalDetail bundles a run with its transcripts. Server returns the
// transcripts in a stable order but we re-sort by (provider, agent) on
// render so the matrix layout is deterministic regardless of insert order.
export type EvalDetail = {
  run: EvalRunSummary;
  transcripts: EvalTranscript[];
};

// BudgetStatus drives the topbar pill. used_/allowed_ are integer token
// counts (no _exact suffix because tokens are already whole units). The
// period bounds are RFC3339 timestamps so the UI can render "resets in
// 12 days" when the org is close to its cap.
export type BudgetStatus = {
  used_tokens_in: number;
  used_tokens_out: number;
  allowed_tokens_in: number;
  allowed_tokens_out: number;
  period_start: string;
  period_end: string;
};

async function failOr<T>(r: Response): Promise<T> {
  if (!r.ok) {
    const body = (await r.json().catch(
      () => ({}) as Record<string, unknown>,
    )) as { error?: string };
    throw new Error(body.error ?? r.statusText);
  }
  return r.json() as Promise<T>;
}

export const evalApi = {
  // list returns every eval run for the workspace, newest first. The list
  // is small (typically <100 rows) so we don't paginate in Phase 5.
  list: async (wsId: string): Promise<EvalRunSummary[]> => {
    const r = await fetch(`${API}/v1/workspaces/${wsId}/eval`, {
      credentials: "include",
      cache: "no-store",
    });
    return failOr<EvalRunSummary[]>(r);
  },

  // get returns the full {run, transcripts} payload for a single run. We
  // re-fetch on every detail-page mount; the response is small and the
  // freshness matters more than caching here.
  get: async (wsId: string, runId: string): Promise<EvalDetail> => {
    const r = await fetch(`${API}/v1/workspaces/${wsId}/eval/${runId}`, {
      credentials: "include",
      cache: "no-store",
    });
    return failOr<EvalDetail>(r);
  },

  // create kicks off a new eval run for a known scenario label. Returns
  // {run_id} immediately (202); the row appears in the list with
  // openai_status / ollama_status = "queued" and the auto-refresh polls
  // it until both providers terminate.
  create: async (wsId: string, scenario: string): Promise<{ run_id: string }> => {
    const r = await fetch(`${API}/v1/workspaces/${wsId}/eval`, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ scenario }),
    });
    return failOr<{ run_id: string }>(r);
  },

  // budget returns the workspace's (really the org's) current period token
  // usage. Used by the topbar pill; polled every 30s. Returns 200 even if
  // the org has never burned a token — the row is auto-created on first
  // call inside the control-plane.
  budget: async (wsId: string): Promise<BudgetStatus> => {
    const r = await fetch(`${API}/v1/workspaces/${wsId}/agents/budget`, {
      credentials: "include",
      cache: "no-store",
    });
    return failOr<BudgetStatus>(r);
  },
};

// SCENARIOS is the whitelist the "Run new eval" dialog offers. Matches
// the eval CLI's `--label` argument; the runner rejects anything off-list
// with a 400. Keep in sync with the backend's catalog in eval/main.go.
export const SCENARIOS: readonly string[] = [
  "schema-drift",
  "null-deref",
  "oom",
] as const;

// formatCentsExact renders a fractional-cents value (cents × 10000) as
// USD with adaptive precision: ≥$0.01 shows 2 decimals; sub-cent shows up
// to 4 so a single Architect call (~$0.002) doesn't read as "$0.00".
// Mirrors the helper in components/billing/UsageSummary.tsx exactly.
export function formatCentsExact(centsExact: number): string {
  const dollars = centsExact / 10000 / 100;
  const decimals = Math.abs(dollars) >= 0.01 || dollars === 0 ? 2 : 4;
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(dollars);
}

// formatTokens renders an integer token count compactly: 1234 → "1.2k",
// 1234567 → "1.2M". For sub-1k counts we render the raw integer so the
// matrix never lies about whether a call cost 99 or 999 tokens.
export function formatTokens(n: number): string {
  if (n < 1000) return n.toString();
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}

// isInFlight is the predicate the list-page auto-refresh consults to
// decide whether to keep polling. We stop the second both providers
// have terminated (succeeded / failed).
export function isInFlight(r: EvalRunSummary): boolean {
  return (
    r.openai_status === "queued" ||
    r.openai_status === "running" ||
    r.ollama_status === "queued" ||
    r.ollama_status === "running"
  );
}
